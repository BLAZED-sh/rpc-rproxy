package proxy

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"net"
	"sync"

	blzdJson "github.com/BLAZED-sh/rpc-rproxy/pkg/json"
	"github.com/rs/zerolog"
)

// DecoderPair tracks a client connection, upstream connection and their decoders
type DecoderPair struct {
	clientConn      net.Conn
	upstreamConn    net.Conn
	clientDecoder   *blzdJson.JsonStreamLexer
	upstreamDecoder *blzdJson.JsonStreamLexer
	createdAt       int64 // Unix timestamp
}

type JsonReverseProxy struct {
	upstream       *Upstream
	listeners      []net.Listener
	context        context.Context
	cancelFunc     context.CancelFunc
	listening      bool
	logger         zerolog.Logger
	asyncCallbacks bool
	bufferSize     int
	maxRead        int

	clientLock   sync.Mutex
	upstreamLock sync.Mutex

	// Tracking active connections and decoders for debugging
	activeConnections      sync.Map // map[string]*DecoderPair
	activeConnectionsCount int64
}

func (j *JsonReverseProxy) Listen() {
	for _, listener := range j.listeners {
		go j.acceptConnections(listener)
	}
	j.listening = true
}

func (j *JsonReverseProxy) Shutdown() {
	j.cancelFunc()

	// Close all listeners
	for _, listener := range j.listeners {
		if err := listener.Close(); err != nil {
			j.logger.Error().Err(err).Msg("Error closing listener")
		}
	}

	j.logger.Info().Msg("Proxy shutdown complete")
}

// DumpDebugInfo returns debug information about active connections and decoders
func (j *JsonReverseProxy) DumpDebugInfo() {
	count := 0

	j.logger.Info().Int64("active_connections_count", j.activeConnectionsCount).Msg("Debug information")

	j.activeConnections.Range(func(key, value interface{}) bool {
		count++
		connID := key.(string)
		pair := value.(*DecoderPair)

		// Get client decoder state
		clientBufferInfo := fmt.Sprintf("Buffer length: %d, cursor: %d, capacity: %d",
			pair.clientDecoder.BufferLength(),
			pair.clientDecoder.Cursor(),
			cap(pair.clientDecoder.Buffer()))

		// Get client buffer content preview
		clientBufferContent := pair.clientDecoder.BufferContent()

		// Get upstream decoder state
		upstreamBufferInfo := fmt.Sprintf("Buffer length: %d, cursor: %d, capacity: %d",
			pair.upstreamDecoder.BufferLength(),
			pair.upstreamDecoder.Cursor(),
			cap(pair.upstreamDecoder.Buffer()))

		// Get upstream buffer content preview
		upstreamBufferContent := pair.upstreamDecoder.BufferContent()

		j.logger.Info().
			Str("connection_id", connID).
			Str("client_buffer", clientBufferInfo).
			Str("client_buffer_content", clientBufferContent).
			Str("upstream_buffer", upstreamBufferInfo).
			Str("upstream_buffer_content", upstreamBufferContent).
			Str("client_remote", pair.clientConn.RemoteAddr().String()).
			Str("upstream_remote", pair.upstreamConn.RemoteAddr().String()).
			Msg("Connection debug info")

		return true
	})

	j.logger.Info().Int("actual_count", count).Msg("Finished dumping debug info")
}

func NewUnixUpstreamJsonRpcProxy(path string, asyncCallbacks bool, multiplexing bool, bufferSize int, maxRead int) *JsonReverseProxy {
	upstream := Upstream{
		pool:      []net.Conn{},
		poolSize:  1,
		multiplex: multiplexing,
		dial: func() (net.Conn, error) {
			return net.Dial("unix", path)
		},
	}

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

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
		context:        cancelCtx,
		cancelFunc:     cancelFunc,
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
		context.Background(),
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
		context.Background(),
		upstream,
		j.bufferSize,
		j.maxRead,
		j.asyncCallbacks,
	)

	// Store connection info for debugging
	decoderPair := &DecoderPair{
		clientConn:      conn,
		upstreamConn:    upstream,
		clientDecoder:   clientDecoder,
		upstreamDecoder: upstreamDecoder,
		createdAt:       time.Now().Unix(),
	}
	j.activeConnections.Store(connID, decoderPair)
	atomic.AddInt64(&j.activeConnectionsCount, 1)

	j.logger.Trace().Str("connID", connID).Msg("Handling connection")

	// Clean up function to remove connection from tracking map when done
	cleanup := func() {
		j.activeConnections.Delete(connID)
		atomic.AddInt64(&j.activeConnectionsCount, -1)
		j.logger.Trace().Str("connID", connID).Msg("Connection closed")
	}

	/*
	   TODO: The decoder callbacks will block till they are done so make this async in the future
	*/
	go upstreamDecoder.DecodeAll(func(b []byte) {
		err := j.handleMessage(b, conn, 1)
		if err != nil {
			j.logger.Error().Err(err).Str("connID", connID).Msg("Error forwarding upstream message to client")
		}
	}, func(err error) {
		j.logger.Error().Err(err).Str("connID", connID).Msg("Error reading from upstream")
		cleanup()
	})

	clientDecoder.DecodeAll(func(b []byte) {
		err := j.handleMessage(b, upstream, 0)
		if err != nil {
			j.logger.Error().Err(err).Str("connID", connID).Msg("Error forwarding client message to upstream")
		}
	}, func(err error) {
		j.logger.Error().Err(err).Str("connID", connID).Msg("Error reading from client")
		cleanup()
	})
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
