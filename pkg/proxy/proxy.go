package proxy

import (
	"context"
	"log"
	"net"
	"sync"

	blzdJson "github.com/BLAZED-sh/rpc-rproxy/pkg/json"
)

type JsonReverseProxy struct {
	upstream   *Upstream
	listeners  []net.Listener
	context    context.Context
	cancelFunc context.CancelFunc
	listening  bool
}

func (j *JsonReverseProxy) Listen() error {
	for _, listener := range j.listeners {
		go j.acceptConnections(listener)
	}
	j.listening = true
	return nil
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
	proxy := JsonReverseProxy{
		&upstream,
		[]net.Listener{},
		cancelCtx,
		cancelFunc,
		false,
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
			return
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

	upstream, err := j.upstream.Conn()
	if err != nil {
		log.Println("Error getting upstream connection", err)
		return
	}
	upstreamDecoder := blzdJson.NewJsonStreamLexer(
		context.Background(),
		upstream,
		16384,
		4096,
	)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		upstreamDecoder.DecodeAll(func(b []byte) {
			err := j.handleMessage(b, conn, 1)
			if err != nil {
				log.Println("Error handling upstream message", err)
			}
		}, func(err error) {
			log.Println("Error reading from upstream", err)
		})
		wg.Done()
	}()

	go func() {
		clientDecoder.DecodeAll(func(b []byte) {
			err := j.handleMessage(b, upstream, 0)
			if err != nil {
				log.Println("Error handling client message", err)
			}
		}, func(err error) {
			log.Println("Error reading from client", err)
		})
		wg.Done()
	}()
	wg.Wait()
}

func (j *JsonReverseProxy) handleMessage(data []byte, output net.Conn, logType byte) error {
	data = append(data, '\n')

	// output.SetWriteDeadline(time.Now().Add(time.Second))
	if _, err := output.Write(data); err != nil {
		return err
	}

	direction := "Client -> Upstream"
	if logType == 1 {
		direction = "Upstream -> Client"
	}

	// s.logger.Debug().
	// 	Int("size", len(data)).
	// 	Str("body", string(data)).
	// 	Msgf("<%s>", direction)

	log.Println("size", len(data), "body", string(data), "<", direction)

	// Process message asynchronously
	//go s.processMessage(data, logType, time.Now())

	return nil
}
