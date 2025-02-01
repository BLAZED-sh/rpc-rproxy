package proxy

type Session struct {
	upstream *Upstream
}

func NewSession() *Session {
	return &Session{}
}
