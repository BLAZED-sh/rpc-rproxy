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

### Parsing limits

Framing limits are configurable per lexer via `json.Limits`, where a zero field
means the limit is not enforced. `DefaultLimits()` is permissive enough for real
Ethereum responses, `StrictLimits()` suits untrusted input, and `Unlimited()`
turns everything off. The proxy applies them per direction through
`ClientLimits` and `UpstreamLimits`, so a caller can be bounded tightly while
the upstream node stays free to answer with whatever `eth_getLogs` produces.

`MaxObjectSize` is the limit that bounds memory, since a whole object has to be
buffered before it can be handed to a callback. The array and object counts are
opt-in and count `[` and `{` tokens rather than elements.

### Benchmarks

Run them with:
```
go test ./pkg/json/ ./pkg/proxy/ -run XXX -bench . -benchmem
```
Do not pass a small `-benchtime`. Both suites have fixed costs that only make
sense amortized, and the concurrent proxy benchmark additionally spawns
`concurrency * GOMAXPROCS` goroutines per run, so at `-benchtime 1x` it measures
goroutine fan-out rather than proxying.

JSON Stream Lexer / Seperator:
```
goos: linux
goarch: amd64
cpu: 11th Gen Intel(R) Core(TM) i7-11850H @ 2.50GHz
BenchmarkDecodeAll/small_objects-16                  5389    221318 ns/op    132.84 MB/s     32800 B/op     3 allocs/op
BenchmarkDecodeAll/medium_array-16                  20178     57232 ns/op    520.36 MB/s     32800 B/op     3 allocs/op
BenchmarkDecodeAll/large_nested_objects-16          20889     57550 ns/op    531.63 MB/s     32800 B/op     3 allocs/op
```
Every iteration is "parsing" about 32k bytes of json. There may be a SIMD
version of this in the future.

`BenchmarkLogs*` frames a single `eth_getLogs` response of the given size, which
is the case that matters for large replies. Throughput staying flat as the
payload grows is the point of the benchmark: framing is linear in object size.
```
BenchmarkLogs1k-16       3291      366667 ns/op    432.99 MB/s     520249 B/op      8 allocs/op
BenchmarkLogs10k-16       333     3855475 ns/op    413.87 MB/s    4190270 B/op     11 allocs/op
BenchmarkLogs40k-16        79    15749277 ns/op    406.09 MB/s   16773239 B/op     13 allocs/op
```

JSON RPC Reverse Proxy (Unix -> Unix):
```
goos: linux
goarch: amd64
cpu: 11th Gen Intel(R) Core(TM) i7-11850H @ 2.50GHz
BenchmarkProxyLinear/BlockNumber-16            181366      6346 ns/op     16.86 MB/s      144 B/op     4 allocs/op
BenchmarkProxyLinear/GetBalance-16             172377      6506 ns/op     26.28 MB/s      224 B/op     4 allocs/op
BenchmarkProxyLinear/GetBlock-16               142495      8631 ns/op    109.83 MB/s     1024 B/op     4 allocs/op
BenchmarkProxyConcurrent/BlockNumber_10-16     648181      1765 ns/op     60.62 MB/s      144 B/op     4 allocs/op
BenchmarkProxyConcurrent/BlockNumber_100-16    406688      2966 ns/op     36.08 MB/s      146 B/op     4 allocs/op
BenchmarkProxyConcurrent/GetBalance_10-16      732426      1871 ns/op     91.38 MB/s      224 B/op     4 allocs/op
BenchmarkProxyConcurrent/GetBlock_10-16        711166      1949 ns/op    486.48 MB/s     1024 B/op     4 allocs/op
```
