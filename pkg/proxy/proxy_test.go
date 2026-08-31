package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func init() {
	zerolog.SetGlobalLevel(zerolog.DebugLevel)
}

// Helper function to create temporary Unix socket path
func getTempSocketPath() string {
	rndString := fmt.Sprintf("%06x", rand.Intn(0xffffff))
	return filepath.Join(
		os.TempDir(),
		fmt.Sprintf("test-socket-%d-%s.sock", time.Now().UnixNano(), rndString),
	)
}

func TestNewUnixUpstreamJsonRpcProxy(t *testing.T) {
	socketPath := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(socketPath, false, false, 4096, 4096)

	assert.NotNil(t, proxy)
	assert.NotNil(t, proxy.upstream)
	assert.Equal(t, 1, proxy.upstream.poolSize)
	assert.False(t, proxy.listening)
}

func TestAddUnixSocketListener(t *testing.T) {
	socketPath := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(socketPath, false, false, 4096, 4096)

	listenerPath := getTempSocketPath()
	err := proxy.AddUnixSocketListener(context.Background(), listenerPath)
	assert.NoError(t, err)
	assert.Len(t, proxy.listeners, 1)

	// Cleanup
	os.Remove(listenerPath)
}

func TestListen(t *testing.T) {
	socketPath := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(socketPath, false, false, 4096, 4096)

	listenerPath := getTempSocketPath()
	err := proxy.AddUnixSocketListener(context.Background(), listenerPath)
	assert.NoError(t, err)

	proxy.Listen()
	assert.True(t, proxy.listening)

	// Cleanup
	os.Remove(listenerPath)
}

// Integration test that creates a mock Ethereum node and tests JSON-RPC communication
func TestIntegrationJsonRpcProxy(t *testing.T) {
	// Create mock Ethereum node (upstream) socket
	upstreamSocket := getTempSocketPath()
	upstreamListener, err := net.Listen("unix", upstreamSocket)
	assert.NoError(t, err)
	defer upstreamListener.Close()
	defer os.Remove(upstreamSocket)

	// Create proxy listener socket
	proxySocket := getTempSocketPath()
	defer os.Remove(proxySocket)

	// Setup proxy
	proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket, false, false, 4096, 4096)
	err = proxy.AddUnixSocketListener(context.Background(), proxySocket)
	assert.NoError(t, err)
	proxy.Listen()

	// Handle mock upstream connections
	go func() {
		conn, err := upstreamListener.Accept()
		if err != nil {
			return
		}
		handleMockEthNode(t, conn)
	}()

	// Test client connection and JSON-RPC communication
	client, err := net.Dial("unix", proxySocket)
	assert.NoError(t, err)
	defer client.Close()

	// Test eth_blockNumber request
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "eth_blockNumber",
		"params":  []interface{}{},
		"id":      1,
	}

	requestBytes, err := json.Marshal(request)
	assert.NoError(t, err)
	requestBytes = append(requestBytes, '\n')

	_, err = client.Write(requestBytes)
	assert.NoError(t, err)

	// Read response
	response := make([]byte, 1024)
	n, err := client.Read(response)
	assert.NoError(t, err)

	var responseObj map[string]interface{}
	err = json.Unmarshal(response[:n], &responseObj)
	assert.NoError(t, err)

	// Verify response
	assert.Equal(t, "2.0", responseObj["jsonrpc"])
	assert.Equal(t, float64(1), responseObj["id"])
	assert.Equal(t, "0x1234", responseObj["result"])
}

// Mock Ethereum node handler
func handleMockEthNode(t *testing.T, conn net.Conn) {
	defer conn.Close()

	buffer := make([]byte, 1024)
	n, err := conn.Read(buffer)
	assert.NoError(t, err)

	var request map[string]interface{}
	err = json.Unmarshal(buffer[:n], &request)
	assert.NoError(t, err)

	// Prepare mock response
	response := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      request["id"],
		"result":  "0x1234",
	}

	responseBytes, err := json.Marshal(response)
	assert.NoError(t, err)
	responseBytes = append(responseBytes, '\n')

	_, err = conn.Write(responseBytes)
	assert.NoError(t, err)
}

// handleBenchmarkNode is a simplified version of handleMockEthNode for benchmarks
func handleBenchmarkNode(conn net.Conn, responseTemplate []byte) {
	defer conn.Close()
	buffer := make([]byte, 4096)

	for {
		n, err := conn.Read(buffer)
		if err != nil {
			fmt.Println(err)
			return
		}
		if n == 0 {
			fmt.Println("n == 0")
			return
		}

		_, err = conn.Write(responseTemplate)
		if err != nil {
			fmt.Println(err)
			return
		}
	}
}

// getMockResponse generates different mock responses for different ETH methods
func getMockResponse(method string, id interface{}) []byte {
	var response map[string]interface{}

	switch method {
	case "eth_blockNumber":
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  "0x1234",
		}
	case "eth_getBalance":
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  "0x1234567890abcdef",
		}
	case "eth_getBlockByNumber":
		// Simulate a full block response
		response = map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]interface{}{
				"number":           "0x1234",
				"hash":             "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"parentHash":       "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"nonce":            "0x1234567890abcdef",
				"sha3Uncles":       "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"logsBloom":        "0x00000000000000000000000000000000",
				"transactionsRoot": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"stateRoot":        "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"receiptsRoot":     "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"miner":            "0x1234567890123456789012345678901234567890",
				"difficulty":       "0x1234",
				"totalDifficulty":  "0x1234",
				"extraData":        "0x1234567890abcdef",
				"size":             "0x1234",
				"gasLimit":         "0x1234",
				"gasUsed":          "0x1234",
				"timestamp":        "0x1234",
				"transactions":     []string{},
				"uncles":           []string{},
			},
		}
	}

	responseBytes, _ := json.Marshal(response)
	return append(responseBytes, '\n')
}

// setupBenchmark creates all the necessary mock infrastructure for benchmarking
func setupBenchmark(
	b *testing.B,
	method string,
	concurrency, cpu int,
) ([]net.Conn, []byte, []byte, func()) {
	b.Helper()
	// Setup mock node
	upstreamSocket := getTempSocketPath()
	upstreamListener, err := net.Listen("unix", upstreamSocket)
	if err != nil {
		b.Fatal(err)
	}

	// Setup response template
	responseTemplate := getMockResponse(method, 1)

	clientsN := concurrency * cpu

	// Handle mock node connections
	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				// TODO: improve this!
				if strings.HasSuffix(err.Error(), "use of closed network connection") {
					break
				}

				b.Logf("Error accepting connection: %v", err)
			}
			go handleBenchmarkNode(conn, responseTemplate)
		}
	}()

	// Setup proxy
	proxySocket := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket, false, false, 4096, 4096)
	err = proxy.AddUnixSocketListener(context.Background(), proxySocket)
	if err != nil {
		b.Fatal(err)
	}
	proxy.Listen()

	// Prepare request template
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  []interface{}{},
		"id":      1,
	}
	if method == "eth_getBalance" {
		request["params"] = []interface{}{"0x1234567890123456789012345678901234567890", "latest"}
	} else if method == "eth_getBlockByNumber" {
		request["params"] = []interface{}{"latest", true}
	}

	requestBytes, _ := json.Marshal(request)
	requestBytes = append(requestBytes, '\n')

	// Create a connection pool
	clients := make([]net.Conn, clientsN)
	for i := 0; i < clientsN; i++ {
		client, err := net.Dial("unix", proxySocket)
		if err != nil {
			b.Fatal(err)
		}
		clients[i] = client
	}

	// Warm every connection with one full round trip before the caller starts
	// its timer.
	warmup := make([]byte, len(responseTemplate))
	for _, client := range clients {
		if _, err := client.Write(requestBytes); err != nil {
			b.Fatal(err)
		}
		for read := 0; read < len(responseTemplate); {
			n, err := client.Read(warmup[read:])
			if err != nil {
				b.Fatal(err)
			}
			read += n
		}
	}

	cleanup := func() {
		for _, client := range clients {
			client.Close()
		}
		upstreamListener.Close()
		os.Remove(upstreamSocket)
		os.Remove(proxySocket)
		//b.Log("cleanup done")
	}

	return clients, requestBytes, responseTemplate, cleanup
}

func BenchmarkProxyLinear(b *testing.B) {
	benchmarks := []struct {
		name   string
		method string
	}{
		{"BlockNumber", "eth_blockNumber"},
		{"GetBalance", "eth_getBalance"},
		{"GetBlock", "eth_getBlockByNumber"},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.StopTimer()
			clients, requestBytes, responseTemplate, cleanup := setupBenchmark(b, bm.method, 1, 1)
			defer cleanup()

			client := clients[0]
			expectedResponseSize := len(responseTemplate)
			response := make([]byte, expectedResponseSize)

			itSize := int64(len(requestBytes) + expectedResponseSize)
			b.SetBytes(itSize)
			b.StartTimer()

			for i := 0; i < b.N; i++ {
				// Write request
				if _, err := client.Write(requestBytes); err != nil {
					b.Fatal(err)
				}

				// Read response
				bytesRead := 0
				for bytesRead < expectedResponseSize {
					n, err := client.Read(response[bytesRead:])
					if err != nil {
						b.Fatal(err)
					}
					bytesRead += n
				}
			}
		})
	}
}

func BenchmarkProxyConcurrent(b *testing.B) {
	benchmarks := []struct {
		name        string
		method      string
		concurrency int
	}{
		{"BlockNumber_10", "eth_blockNumber", 10},
		{"BlockNumber_100", "eth_blockNumber", 100},
		{"GetBalance_10", "eth_getBalance", 10},
		{"GetBlock_10", "eth_getBlockByNumber", 10},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			b.StopTimer()

			cpu := runtime.GOMAXPROCS(0)
			clients, requestBytes, responseTemplate, cleanup := setupBenchmark(
				b,
				bm.method,
				bm.concurrency,
				cpu,
			)
			defer cleanup()

			// Create a buffered channel to distribute client connections
			clientsN := bm.concurrency * cpu
			clientChan := make(chan net.Conn, clientsN)
			doneChan := make(chan struct{}, clientsN)

			for _, client := range clients {
				clientChan <- client
			}

			b.SetBytes(int64(len(requestBytes) + len(responseTemplate)))
			b.SetParallelism(bm.concurrency)
			b.StartTimer()

			expectedResponseSize := len(responseTemplate)
			b.RunParallel(func(pb *testing.PB) {
				client := <-clientChan

				// Claude:
				// Per-goroutine buffer. This used to be shared across every
				// parallel goroutine, which is a data race and also let a
				// partial read leave the connection out of step with the next
				// iteration.
				response := make([]byte, expectedResponseSize)

				for pb.Next() {
					_, err := client.Write(requestBytes)
					if err != nil {
						b.Error(err)
						break
					}

					for read := 0; read < expectedResponseSize; {
						n, err := client.Read(response[read:])
						if err != nil {
							b.Error(err)
							break
						}
						read += n
					}
				}

				// Signal that we're done with this client
				doneChan <- struct{}{}
			})

			//Wait for all clients to finish
			for i := 0; i < clientsN; i++ {
				<-doneChan
			}
		})
	}
}

// A half-closing client (socat -t) must still get its answer.
func TestClientHalfCloseStillGetsResponse(t *testing.T) {
	forEachCallbackMode(t, func(t *testing.T, async bool) {
		upstreamSocket := getTempSocketPath()
		upstreamListener, err := net.Listen("unix", upstreamSocket)
		assert.NoError(t, err)
		defer upstreamListener.Close()
		defer os.Remove(upstreamSocket)

		proxySocket := getTempSocketPath()
		defer os.Remove(proxySocket)

		proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket, async, false, 4096, 4096)
		assert.NoError(t, proxy.AddUnixSocketListener(context.Background(), proxySocket))
		proxy.Listen()

		go func() {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			buf := make([]byte, 1024)
			if _, err := conn.Read(buf); err != nil {
				return
			}
			// Answer only after the client has stopped sending.
			time.Sleep(100 * time.Millisecond)
			conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1234"}` + "\n"))
		}()

		client, err := net.Dial("unix", proxySocket)
		assert.NoError(t, err)
		defer client.Close()

		_, err = client.Write([]byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}` + "\n"))
		assert.NoError(t, err)
		assert.NoError(t, client.(*net.UnixConn).CloseWrite())

		assert.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
		response := make([]byte, 1024)
		n, err := client.Read(response)
		assert.NoError(t, err)
		assert.Contains(t, string(response[:n]), "0x1234")
	})
}

// A departed client must not leave its upstream socket and goroutine behind.
func TestUpstreamClosesWhenClientLeaves(t *testing.T) {
	forEachCallbackMode(t, func(t *testing.T, async bool) {
		upstreamSocket := getTempSocketPath()
		upstreamListener, err := net.Listen("unix", upstreamSocket)
		assert.NoError(t, err)
		defer upstreamListener.Close()
		defer os.Remove(upstreamSocket)

		proxySocket := getTempSocketPath()
		defer os.Remove(proxySocket)

		proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket, async, false, 4096, 4096)
		assert.NoError(t, proxy.AddUnixSocketListener(context.Background(), proxySocket))
		proxy.Listen()

		upstreamGone := make(chan struct{})
		go func() {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()

			buf := make([]byte, 1024)
			if _, err := conn.Read(buf); err != nil {
				return
			}
			conn.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":"0x1234"}` + "\n"))

			// Blocks until the proxy lets go of its end.
			if _, err := conn.Read(buf); err != nil {
				close(upstreamGone)
			}
		}()

		client, err := net.Dial("unix", proxySocket)
		assert.NoError(t, err)

		_, err = client.Write([]byte(`{"jsonrpc":"2.0","method":"eth_blockNumber","params":[],"id":1}` + "\n"))
		assert.NoError(t, err)

		assert.NoError(t, client.SetReadDeadline(time.Now().Add(3*time.Second)))
		if _, err := client.Read(make([]byte, 1024)); err != nil {
			t.Fatalf("no response from the proxy: %v", err)
		}
		client.Close()

		select {
		case <-upstreamGone:
		case <-time.After(upstreamDrainTimeout + 3*time.Second):
			t.Fatal("the upstream connection outlived the client")
		}
	})
}

// forEachCallbackMode runs a case both ways; production uses async.
func forEachCallbackMode(t *testing.T, run func(t *testing.T, async bool)) {
	t.Helper()
	for _, mode := range []struct {
		name  string
		async bool
	}{
		{"sync-callbacks", false},
		{"async-callbacks", true},
	} {
		t.Run(mode.name, func(t *testing.T) { run(t, mode.async) })
	}
}
