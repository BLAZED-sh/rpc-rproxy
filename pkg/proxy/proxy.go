package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync/atomic"
	"time"

	"net"
	"sync"

	blzdJson "github.com/BLAZED-sh/rpc-rproxy/pkg/json"
	"github.com/rs/zerolog"
)

type ProxyConn struct {
	clientConn      net.Conn
	upstreamConn    net.Conn
	clientDecoder   *blzdJson.JsonStreamLexer
	upstreamDecoder *blzdJson.JsonStreamLexer
	createdAt       int64 // Unix timestamp
}

type JsonReverseProxy struct {
	upstream       *Upstream
	listeners      []net.Listener
	listening      bool
	logger         zerolog.Logger
	asyncCallbacks bool
	bufferSize     int
	maxRead        int

	// Optional callbacks for connection events
	OnConnect    func(id string, conn *ProxyConn)
	OnDisconnect func(id string, conn *ProxyConn)
	OnRequest    func(id string, conn *ProxyConn, data []byte)
	OnResponse   func(id string, conn *ProxyConn, data []byte)

	clientLock   sync.Mutex
	upstreamLock sync.Mutex

	// Tracking active connections and decoders for debugging
	activeConnections      sync.Map // map[string]*DecoderPair
	ActiveConnectionsCount int64
}

func (j *JsonReverseProxy) Listen() {
	for _, listener := range j.listeners {
		go j.acceptConnections(listener)
	}
	j.listening = true
}

func (j *JsonReverseProxy) Shutdown() {
	// Close all listeners
	for _, listener := range j.listeners {
		if err := listener.Close(); err != nil {
			j.logger.Error().Err(err).Msg("Error closing listener")
		}
	}

	// Close all active connections
	j.activeConnections.Range(func(key, value interface{}) bool {
		connID := key.(string)
		conn := value.(*ProxyConn)
		if err := conn.clientConn.Close(); err != nil {
			j.logger.Error().Err(err).Str("connID", connID).Msg("Error closing client connection")
		}
		if err := conn.upstreamConn.Close(); err != nil {
			j.logger.Error().Err(err).Str("connID", connID).Msg("Error closing upstream connection")
		}
		// TODO: check if the disconnect will trigger eof and through that the cancel function
		//j.activeConnections.Delete(key)
		return true
	})

	j.logger.Info().Msg("Proxy shutdown complete")
}

// DumpDebugInfo returns debug information about active connections and decoders
func (j *JsonReverseProxy) DumpDebugInfo() {
	count := 0

	j.logger.Info().
		Int64("active_connections_count", j.ActiveConnectionsCount).
		Msg("Debug information")

	j.activeConnections.Range(func(key, value interface{}) bool {
		count++
		connID := key.(string)
		conn := value.(*ProxyConn)

		// Get client decoder state
		clientBufferInfo := fmt.Sprintf("Buffer length: %d, cursor: %d, capacity: %d",
			conn.clientDecoder.BufferLength(),
			conn.clientDecoder.Cursor(),
			cap(conn.clientDecoder.Buffer()))

		// Get client buffer content preview
		clientBufferContent := conn.clientDecoder.BufferContent()

		// Get upstream decoder state
		upstreamBufferInfo := fmt.Sprintf("Buffer length: %d, cursor: %d, capacity: %d",
			conn.upstreamDecoder.BufferLength(),
			conn.upstreamDecoder.Cursor(),
			cap(conn.upstreamDecoder.Buffer()))

		// Get upstream buffer content preview
		upstreamBufferContent := conn.upstreamDecoder.BufferContent()

		j.logger.Info().
			Str("connection_id", connID).
			Str("client_buffer", clientBufferInfo).
			Str("client_buffer_content", clientBufferContent).
			Str("upstream_buffer", upstreamBufferInfo).
			Str("upstream_buffer_content", upstreamBufferContent).
			Str("client_remote", conn.clientConn.RemoteAddr().String()).
			Str("upstream_remote", conn.upstreamConn.RemoteAddr().String()).
			Msg("Connection debug info")

		return true
	})

	j.logger.Info().Int("actual_count", count).Msg("Finished dumping debug info")
}

func NewUnixUpstreamJsonRpcProxy(
	path string,
	asyncCallbacks bool,
	multiplexing bool,
	bufferSize int,
	maxRead int,
) *JsonReverseProxy {
	upstream := Upstream{
		pool:      []net.Conn{},
		poolSize:  1,
		multiplex: multiplexing,
		dial: func() (net.Conn, error) {
			return net.Dial("unix", path)
		},
	}

	// Initialize a new logger
	logger := zerolog.New(zerolog.NewConsoleWriter()).
		Level(zerolog.GlobalLevel()).
		With().
		Timestamp().
		Str("component", "proxy").
		Logger()

	proxy := JsonReverseProxy{
		upstream:       &upstream,
		listeners:      []net.Listener{},
		listening:      false,
		logger:         logger,
		asyncCallbacks: asyncCallbacks,
		bufferSize:     bufferSize,
		maxRead:        maxRead,
	}
	return &proxy
}

func (j *JsonReverseProxy) AddUnixSocketListener(context context.Context, path string) error {
	config := net.ListenConfig{}
	var listener net.Listener
	listener, err := config.Listen(context, "unix", path)
	if err != nil {
		return err
	}
	j.listeners = append(j.listeners, listener)
	return nil
}

func (j *JsonReverseProxy) acceptConnections(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}

			j.logger.Error().Err(err).Msg("Error accepting connection")
			continue
		}
		go j.handleConnection(conn)
	}
}

func (j *JsonReverseProxy) handleConnection(conn net.Conn) {
	// Generate a unique connection ID
	connID := fmt.Sprintf("conn_%d", time.Now().UnixNano())

	clientDecoder := blzdJson.NewJsonStreamLexer(
		conn,
		j.bufferSize,
		j.maxRead,
		j.asyncCallbacks,
	)

	upstream, err := j.upstream.NewConn()
	if err != nil {
		j.logger.Error().Err(err).Msg("Error getting upstream connection")
		return
	}
	upstreamDecoder := blzdJson.NewJsonStreamLexer(
		upstream,
		j.bufferSize,
		j.maxRead,
		j.asyncCallbacks,
	)

	// Store connection info for debugging
	decoderPair := &ProxyConn{
		clientConn:      conn,
		upstreamConn:    upstream,
		clientDecoder:   clientDecoder,
		upstreamDecoder: upstreamDecoder,
		createdAt:       time.Now().Unix(),
	}
	j.activeConnections.Store(connID, decoderPair)
	atomic.AddInt64(&j.ActiveConnectionsCount, 1)

	j.logger.Trace().Str("connID", connID).Msg("Handling connection")

	// Call the OnConnect callback if set
	if j.OnConnect != nil {
		go j.OnConnect(connID, decoderPair)
	}

	ctx, cancelFn := context.WithCancelCause(context.Background())

	// TODO: close other side if error happens on one side
	go upstreamDecoder.DecodeAll(ctx, func(b []byte) {
		err := j.handleMessage(b, conn, 1)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				j.logger.Debug().
					Err(err).
					Str("connID", connID).
					Msg("Client->Upstream connection EOF")
			} else {
				j.logger.Error().
					Err(err).
					Str("connID", connID).
					Msg("Error forwarding upstream message to client.")
			}

			cancelFn(err)
			return
		}

		// Call the OnResponse callback if set
		if j.OnResponse != nil {
			go j.OnResponse(connID, decoderPair, b)
		}
	}, func(err error) {
		j.logger.Error().Err(err).Str("connID", connID).Msg("Error reading from upstream")
		cancelFn(err)
	})

	clientDecoder.DecodeAll(ctx, func(b []byte) {
		err := j.handleMessage(b, upstream, 0)
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				j.logger.Debug().
					Err(err).
					Str("connID", connID).
					Msg("Upstream->Client connection EOF")
			} else {
				// TODO: Implement upstream reconnect, maybe resubscribe
				j.logger.Error().
					Err(err).
					Str("connID", connID).
					Msg("Error forwarding client message to upstream")
			}

			cancelFn(err)
			return
		}

		if j.OnRequest != nil {
			go j.OnRequest(connID, decoderPair, b)
		}
	}, func(err error) {
		j.logger.Error().Err(err).Str("connID", connID).Msgf("Error reading from client")
		cancelFn(err)
	})

	if j.OnDisconnect != nil {
		go j.OnDisconnect(connID, decoderPair)
	}

	j.activeConnections.Delete(connID)
	atomic.AddInt64(&j.ActiveConnectionsCount, -1)
	j.logger.Trace().Str("connID", connID).Msg("Connection closed")
}

func (j *JsonReverseProxy) handleMessage(data []byte, output net.Conn, logType byte) error {
	data = append(data, '\n')
	if _, err := output.Write(data); err != nil {
		return err
	}

	direction := "Client -> Upstream"
	if logType == 1 {
		direction = "Upstream -> Client"
	}

	j.logger.Trace().
		Int("size", len(data)).
		Str("body", string(data)).
		Msgf("<%s>", direction)

	//go s.processMessage(data, logType, time.Now())

	return nil
}
