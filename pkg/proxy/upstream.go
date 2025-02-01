package proxy

import "net"

type Upstream struct {
	pool []net.Conn
}
