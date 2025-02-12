package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// Helper function to create temporary Unix socket path
func getTempSocketPath() string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("test-socket-%d.sock", time.Now().UnixNano()))
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

	err = proxy.Listen()
	assert.NoError(t, err)
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
	err = proxy.Listen()
	assert.NoError(t, err)

	// Handle mock upstream connections
	go func() {
		for {
			conn, err := upstreamListener.Accept()
			if err != nil {
				return
			}
			go handleMockEthNode(t, conn)
		}
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
		_, err := conn.Read(buffer)
		if err != nil {
			return
		}
		_, err = conn.Write(responseTemplate)
		if err != nil {
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
				"hash":            "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"parentHash":      "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"nonce":           "0x1234567890abcdef",
				"sha3Uncles":      "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"logsBloom":       "0x00000000000000000000000000000000",
				"transactionsRoot": "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"stateRoot":       "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"receiptsRoot":    "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef",
				"miner":           "0x1234567890123456789012345678901234567890",
				"difficulty":      "0x1234",
				"totalDifficulty": "0x1234",
				"extraData":       "0x1234567890abcdef",
				"size":           "0x1234",
				"gasLimit":       "0x1234",
				"gasUsed":        "0x1234",
				"timestamp":      "0x1234",
				"transactions":   []string{},
				"uncles":        []string{},
			},
		}
	}

	responseBytes, _ := json.Marshal(response)
	return append(responseBytes, '\n')
}

func BenchmarkProxy(b *testing.B) {
	benchmarks := []struct {
		name         string
		method       string
		concurrency  int
		messageCount int
	}{
		{"BlockNumber_Single", "eth_blockNumber", 1, 1000},
		{"BlockNumber_Concurrent10", "eth_blockNumber", 10, 1000},
		{"BlockNumber_Concurrent100", "eth_blockNumber", 100, 1000},
		{"GetBalance_Single", "eth_getBalance", 1, 1000},
		{"GetBalance_Concurrent10", "eth_getBalance", 10, 1000},
		{"GetBlock_Single", "eth_getBlockByNumber", 1, 1000},
		{"GetBlock_Concurrent10", "eth_getBlockByNumber", 10, 1000},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Setup mock node
			upstreamSocket := getTempSocketPath()
			upstreamListener, err := net.Listen("unix", upstreamSocket)
			if err != nil {
				b.Fatal(err)
			}
			defer upstreamListener.Close()
			defer os.Remove(upstreamSocket)

			// Setup response template
			responseTemplate := getMockResponse(bm.method, 1)

			// Handle mock node connections
			go func() {
				for {
					conn, err := upstreamListener.Accept()
					if err != nil {
						return
					}
					go handleBenchmarkNode(conn, responseTemplate)
				}
			}()

			// Setup proxy
			proxySocket := getTempSocketPath()
			defer os.Remove(proxySocket)

			proxy := NewUnixUpstreamJsonRpcProxy(upstreamSocket)
			err = proxy.AddUnixSocketListener(context.Background(), proxySocket)
			if err != nil {
				b.Fatal(err)
			}
			err = proxy.Listen()
			if err != nil {
				b.Fatal(err)
			}

			// Prepare request template
			request := map[string]interface{}{
				"jsonrpc": "2.0",
				"method":  bm.method,
				"params":  []interface{}{},
				"id":      1,
			}
			if bm.method == "eth_getBalance" {
				request["params"] = []interface{}{"0x1234567890123456789012345678901234567890", "latest"}
			} else if bm.method == "eth_getBlockByNumber" {
				request["params"] = []interface{}{"latest", true}
			}

			requestBytes, _ := json.Marshal(request)
			requestBytes = append(requestBytes, '\n')

			// Reset timer before the actual benchmark
			b.ResetTimer()

			// Run concurrent clients
			done := make(chan bool)
			for c := 0; c < bm.concurrency; c++ {
				go func() {
					client, err := net.Dial("unix", proxySocket)
					if err != nil {
						b.Error(err)
						return
					}
					defer client.Close()

					response := make([]byte, 4096)
					for i := 0; i < bm.messageCount; i++ {
						_, err = client.Write(requestBytes)
						if err != nil {
							b.Error(err)
							return
						}

						_, err = client.Read(response)
						if err != nil {
							b.Error(err)
							return
						}
					}
					done <- true
				}()
			}

			// Wait for all goroutines to complete
			for i := 0; i < bm.concurrency; i++ {
				<-done
			}
		})
	}
}

