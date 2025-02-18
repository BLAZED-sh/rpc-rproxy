package proxy

import (
	"context"

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

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		upstreamDecoder.DecodeAll(func(b []byte) {
			//j.logger.Trace().Msgf("Upstream -> Client: %s", string(b))

			err := j.handleMessage(b, conn, 1)
			if err != nil {
				j.logger.Error().Err(err).Msg("Error handling upstream message")
			}
		}, func(err error) {
			j.logger.Error().Err(err).Msg("Error reading from upstream")
		})
		wg.Done()
	}()

	go func() {
		clientDecoder.DecodeAll(func(b []byte) {
			j.logger.Trace().Msgf("Client -> Upstream: %s", string(b))

			err := j.handleMessage(b, upstream, 0)
			if err != nil {
				j.logger.Error().Err(err).Msg("Error handling client message")
			}
		}, func(err error) {
			j.logger.Error().Err(err).Msg("Error reading from client")
		})
		wg.Done()
	}()
	wg.Wait()
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

	// s.logger.Debug().
	// 	Int("size", len(data)).
	// 	Str("body", string(data)).
	// 	Msgf("<%s>", direction)

	//log.Println("size", len(data), "body", string(data), "<", direction)

	// Process message asynchronously
	//go s.processMessage(data, logType, time.Now())

	return nil
}
