package app

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestBoundedListenerBlocksUntilConnectionCloses(t *testing.T) {
	var listenConfig net.ListenConfig
	inner, err := listenConfig.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := newBoundedListener(inner, 1)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()

	var dialer net.Dialer
	clientOne, err := dialer.DialContext(t.Context(), "tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientOne.Close()
	serverOne, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}

	accepted := make(chan net.Conn, 1)
	go func() {
		conn, _ := listener.Accept()
		accepted <- conn
	}()
	select {
	case <-accepted:
		t.Fatal("accepted a second connection before capacity was released")
	case <-time.After(25 * time.Millisecond):
	}

	if err := serverOne.Close(); err != nil {
		t.Fatal(err)
	}
	clientTwo, err := dialer.DialContext(t.Context(), "tcp", inner.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer clientTwo.Close()
	select {
	case serverTwo := <-accepted:
		if serverTwo == nil {
			t.Fatal("second Accept returned nil")
		}
		_ = serverTwo.Close()
	case <-time.After(time.Second):
		t.Fatal("second Accept remained blocked after capacity was released")
	}
}

func TestBoundedListenerCloseUnblocksAccept(t *testing.T) {
	var listenConfig net.ListenConfig
	inner, err := listenConfig.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listener, err := newBoundedListener(inner, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Accept after Close error = %v, want net.ErrClosed", err)
	}
}
