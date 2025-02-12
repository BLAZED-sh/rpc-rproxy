package proxy

import (
	"math/rand"
	"net"
)

type Upstream struct {
	pool     []net.Conn
	poolSize int
	dial     func() (net.Conn, error)
}

func (u *Upstream) RefillPool() error {
	diff := u.poolSize - len(u.pool)
	if diff == 0 {
		return nil
	}

	for i := 0; i < diff; i++ {
		conn, err := u.dial()
		if err != nil {
			return err
		}

		u.pool = append(u.pool, conn)
	}

	return nil
}

// Return a random upstream from pool
func (u *Upstream) Conn() (net.Conn, error) {
	err := u.RefillPool()
	if err != nil {
		return nil, err
	}

	if u.poolSize == 1 {
		return u.pool[0], nil
	}

	i := rand.Intn(len(u.pool))
	return u.pool[i], nil
}
