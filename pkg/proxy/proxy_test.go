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

	"github.com/stretchr/testify/assert"
)

// Helper function to create temporary Unix socket path
func getTempSocketPath() string {
	rndString := fmt.Sprintf("%06x", rand.Intn(0xffffff))
	return filepath.Join(os.TempDir(), fmt.Sprintf("test-socket-%d-%s.sock", time.Now().UnixNano(), rndString))
}

func TestNewUnixUpstreamJsonRpcProxy(t *testing.T) {
	socketPath := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(socketPath)

	assert.NotNil(t, proxy)
	assert.NotNil(t, proxy.upstream)
	assert.Equal(t, 1, proxy.upstream.poolSize)
	assert.False(t, proxy.listening)
}

func TestAddUnixSocketListener(t *testing.T) {
	socketPath := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(socketPath)

	listenerPath := getTempSocketPath()
	err := proxy.AddUnixSocketListener(context.Background(), listenerPath)
	assert.NoError(t, err)
	assert.Len(t, proxy.listeners, 1)

	// Cleanup
	os.Remove(listenerPath)
}

func TestListen(t *testing.T) {
	socketPath := getTempSocketPath()
	proxy := NewUnixUpstreamJsonRpcProxy(socketPath)

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
	proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket)
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
func setupBenchmark(b *testing.B, method string, concurrency, cpu int) ([]net.Conn, []byte, []byte, func()) {
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
	proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket)
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
			response := make([]byte, 4096)
			expectedResponseSize := len(responseTemplate)

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
			clients, requestBytes, responseTemplate, cleanup := setupBenchmark(b, bm.method, bm.concurrency, cpu)
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

			response := make([]byte, 4096)
			b.RunParallel(func(pb *testing.PB) {
				client := <-clientChan

				for pb.Next() {
					_, err := client.Write(requestBytes)
					if err != nil {
						b.Error(err)
						break
					}

					_, err = client.Read(response)
					if err != nil {
						b.Error(err)
						break
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
