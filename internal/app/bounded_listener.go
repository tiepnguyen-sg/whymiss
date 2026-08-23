package app

import (
	"fmt"
	"net"
	"sync"
)

type boundedListener struct {
	net.Listener
	tokens    chan struct{}
	done      chan struct{}
	closeOnce sync.Once
}

func newBoundedListener(listener net.Listener, maxConnections int) (net.Listener, error) {
	if listener == nil {
		return nil, fmt.Errorf("listener is nil")
	}
	if maxConnections <= 0 {
		return nil, fmt.Errorf("maximum connections must be positive, got %d", maxConnections)
	}
	return &boundedListener{
		Listener: listener,
		tokens:   make(chan struct{}, maxConnections),
		done:     make(chan struct{}),
	}, nil
}

func (l *boundedListener) Accept() (net.Conn, error) {
	select {
	case l.tokens <- struct{}{}:
	case <-l.done:
		return nil, net.ErrClosed
	}

	conn, err := l.Listener.Accept()
	if err != nil {
		<-l.tokens
		return nil, err
	}
	return &boundedConn{Conn: conn, release: func() { <-l.tokens }}, nil
}

func (l *boundedListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return l.Listener.Close()
}

type boundedConn struct {
	net.Conn
	releaseOnce sync.Once
	release     func()
}

func (c *boundedConn) Close() error {
	err := c.Conn.Close()
	c.releaseOnce.Do(c.release)
	return err
}
