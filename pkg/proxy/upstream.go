package proxy

import (
	"bytes"
	"errors"
	"math/rand"
	"net"
	"strconv"
	"sync/atomic"
)

type Upstream struct {
	pool     []net.Conn
	poolSize int
	dial     func() (net.Conn, error)

	multiplex       bool
	multiplexLastId atomic.Uint64
	multiplexedIds  []uint32
}

func (u *Upstream) Intialize() error {
	err := u.RefillPool()
	if err != nil {
		return err
	}

	return nil
}

func (u *Upstream) RefillPool() error {
	diff := u.poolSize - len(u.pool)
	if diff == 0 {
		return nil
	}

	for i := 0; i < diff; i++ {
		conn, err := u.NewConn()
		if err != nil {
			return err
		}

		u.pool = append(u.pool, conn)
	}

	return nil
}

// Return a random upstream from pool
func (u *Upstream) PooledConn() (net.Conn, error) {
	if u.poolSize == 1 {
		return u.pool[0], nil
	}

	i := rand.Intn(len(u.pool))
	return u.pool[i], nil
}

func (u *Upstream) NewConn() (net.Conn, error) {
	return u.dial()
}

func (u *Upstream) WriteMsg(msg []byte, conn net.Conn) (int, error) {
	if u.multiplex {
		var err error
		msg, err = u.multiplexMsg(msg)
		if err != nil {
			return -1, err
		}

	}

	return conn.Write(msg)
}

func (u *Upstream) multiplexMsg(msg []byte) ([]byte, error) {
	// Patch the message with a multiplexed id
	nextId := u.multiplexLastId.Add(1)

	idPos := bytes.Index(msg, []byte(`"id":`))
	if idPos == -1 {
		return nil, errors.New("No id found in original message")
	}
	startPos := -1
	endPos := -1
	for i := idPos + 5; i < len(msg); i++ {
		// Find id string start
		if msg[i] == '\\' {
			return nil, errors.New("Escape character found in id - this is not supported")
		}
		if msg[i] == '"' {
			if startPos == -1 {
				startPos = i
				continue
			}
			endPos = i
			break
		}
	}

	if startPos == -1 || endPos == -1 {
		return nil, errors.New("Id string key found but no (completed) value")
	}

	// Insert the multiplexed id
	idS := strconv.FormatUint(nextId, 10)
	msg = append(msg[:startPos+1], append([]byte(idS), msg[endPos:]...)...)

	// TODO: make this thread safe
	u.multiplexedIds = append(u.multiplexedIds, uint32(nextId))

	return msg, nil
}
