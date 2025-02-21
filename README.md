# rpc-rproxy  
A fast JSON RPC reverse proxy with on the fly parsing and unix domain socket support.  
Supposed to be used with Ethereum Nodes but will work with any JSON RPC protocol.  
  
Under Development - This should not be used by anyone in production yet
  
### TODO
  - [x] Reverse Proxy
    - [x] Unix Domain Socket
    - [ ] HTTP
  - [ ] Session Handling
    - [ ] One to one mode
    - [ ] Pooled mode
    - [x] Single upstream
    - [ ] Graceful disconnects
    - [ ] Reconnects
        - [ ] Pub/Sub Replay
    - [x] Stream Parsing (Lexing / Seperating Objects)
        - [x] Buffered
        - [ ] Instant/Blocking
    - [ ] SQLite Logs

### Benchmarks
JSON Stream Lexer / Seperator:  
```
goos: linux
goarch: amd64
cpu: AMD Ryzen 7 5800X3D 8-Core Processor
BenchmarkDecodeAll
BenchmarkDecodeAll/small_objects
BenchmarkDecodeAll/small_objects-16                15110             78602 ns/op         374.03 MB/s       32768 B/op          1 allocs/op
BenchmarkDecodeAll/medium_array
BenchmarkDecodeAll/medium_array-16                 19052             62818 ns/op         474.08 MB/s       98304 B/op          2 allocs/op
BenchmarkDecodeAll/large_nested_objects
BenchmarkDecodeAll/large_nested_objects-16         19274             62327 ns/op         490.88 MB/s       98304 B/op          2 allocs/op
PASS
ok      command-line-arguments  6.928s
```
Honestly I am already quite happy with the performance we got here. Every iteration is "parsing" about 32k bytes of json. There may be a SMID version of this in the future.  
  
JSON RPC Reverse Proxy (Unix -> Unix):   
```
goos: linux
goarch: amd64
cpu: AMD Ryzen 7 5800X3D 8-Core Processor
BenchmarkProxyLinear
BenchmarkProxyLinear/BlockNumber
BenchmarkProxyLinear/BlockNumber-16               114567              9839 ns/op          10.87 MB/s           0 B/op          0 allocs/op
BenchmarkProxyLinear/GetBalance
BenchmarkProxyLinear/GetBalance-16                118783              9787 ns/op          17.47 MB/s           0 B/op          0 allocs/op
BenchmarkProxyLinear/GetBlock
BenchmarkProxyLinear/GetBlock-16                  108301             11018 ns/op          86.04 MB/s           0 B/op          0 allocs/op
BenchmarkProxyConcurrent
BenchmarkProxyConcurrent/BlockNumber_10
BenchmarkProxyConcurrent/BlockNumber_10-16        903729              1182 ns/op          90.52 MB/s           0 B/op          0 allocs/op
BenchmarkProxyConcurrent/BlockNumber_100
BenchmarkProxyConcurrent/BlockNumber_100-16      1203837               994.2 ns/op       107.62 MB/s           1 B/op          0 allocs/op
BenchmarkProxyConcurrent/GetBalance_10
BenchmarkProxyConcurrent/GetBalance_10-16         964677              1185 ns/op         144.29 MB/s           4 B/op          0 allocs/op
BenchmarkProxyConcurrent/GetBlock_10
BenchmarkProxyConcurrent/GetBlock_10-16           876962              1252 ns/op         757.41 MB/s           0 B/op          0 allocs/op
PASS
ok      command-line-arguments  9.496s
```
The benchmark test of this is highly WIP and may return totaly bogus numbers.
