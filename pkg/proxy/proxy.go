package proxy

import (
	"context"
	"errors"

	"net"
	"sync"

	blzdJson "github.com/BLAZED-sh/rpc-rproxy/pkg/json"
	"github.com/rs/zerolog"
)

type JsonReverseProxy struct {
	upstream   *Upstream
	listeners  []net.Listener
	context    context.Context
	cancelFunc context.CancelFunc
	listening  bool
	logger     zerolog.Logger

	clientLock   sync.Mutex
	upstreamLock sync.Mutex
}

func (j *JsonReverseProxy) Listen() {
	for _, listener := range j.listeners {
		go j.acceptConnections(listener)
	}
	j.listening = true
}

func NewUnixUpstreamJsonRpcProxy(path string) *JsonReverseProxy {
	upstream := Upstream{
		pool:     []net.Conn{},
		poolSize: 1,
		dial: func() (net.Conn, error) {
			return net.Dial("unix", path)
		},
	}

	cancelCtx, cancelFunc := context.WithCancel(context.Background())

	// Initialize a new logger
	logger := zerolog.New(zerolog.NewConsoleWriter()).
		Level(zerolog.DebugLevel).
		With().
		Timestamp().
		Str("component", "proxy").
		Logger()

	proxy := JsonReverseProxy{
		upstream:   &upstream,
		listeners:  []net.Listener{},
		context:    cancelCtx,
		cancelFunc: cancelFunc,
		listening:  false,
		logger:     logger,
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
	clientDecoder := blzdJson.NewJsonStreamLexer(
		context.Background(),
		conn,
		16384,
		4096,
	)

	upstream, err := j.upstream.NewConn()
	if err != nil {
		j.logger.Error().Err(err).Msg("Error getting upstream connection")
		return
	}
	upstreamDecoder := blzdJson.NewJsonStreamLexer(
		context.Background(),
		upstream,
		16384,
		4096,
	)

	j.logger.Trace().Msg("Handling connection")

        /*
        TODO: The decoder callbacks will block till they are done so make this async in the future
        */
	go upstreamDecoder.DecodeAll(func(b []byte) {
		err := j.handleMessage(b, conn, 1)
		if err != nil {
			j.logger.Error().Err(err).Msg("Error forwarding upstream message to client")
		}
	}, func(err error) {
		j.logger.Error().Err(err).Msg("Error reading from upstream")
	})

	clientDecoder.DecodeAll(func(b []byte) {
		err := j.handleMessage(b, upstream, 0)
		if err != nil {
			j.logger.Error().Err(err).Msg("Error forwarding client message to upstream")
		}
	}, func(err error) {
		j.logger.Error().Err(err).Msg("Error reading from client")
	})
}

func (j *JsonReverseProxy) handleMessage(data []byte, output net.Conn, logType byte) error {
	data = append(data, '\n')
	if _, err := output.Write(data); err != nil {
		return err
	}

	// direction := "Client -> Upstream"
	// if logType == 1 {
	// 	direction = "Upstream -> Client"
	// }

	// j.logger.Trace().
	// 	Int("size", len(data)).
	// 	Str("body", string(data)).
	// 	Msgf("<%s>", direction)

	//go s.processMessage(data, logType, time.Now())

	return nil
}
