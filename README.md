# rpc-rproxy  
A fast JSON RPC reverse proxy with on the fly parsing and unix domain socket support.  
Supposed to be used with Ethereum Nodes but will work with any JSON RPC protocol.  
  
Under Development  
  
  
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
    - [x] Parsing
        - [x] Buffered
        - [ ] Instant/Blocking
    - [ ] SQLite Logs
