package clock

import (
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

// fakeServer is a real SNTP server on loopback: it speaks the actual wire protocol
// over a real UDP socket, rather than mocking the query() function. What it
// fabricates is only its own clock — the skew a test wants to inject — never the
// protocol exchange itself.
type fakeServer struct {
	conn *net.UDPConn
	addr string

	mu          sync.Mutex
	skew        time.Duration // added to the local clock to produce "the server's time"
	stratum     byte
	leap        byte
	respond     bool // if false, requests are read and silently dropped
	shortLen    int  // if > 0, replies are truncated to this many bytes
	mode        byte // if nonzero, overrides the reply's Mode field
	wrongOrigin bool // if true, the reply echoes a bogus Origin Timestamp
}

func newFakeServer(t *testing.T, skew time.Duration) *fakeServer {
	t.Helper()

	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("listen udp: %v", err)
	}

	s := &fakeServer{conn: conn, addr: conn.LocalAddr().String(), skew: skew, stratum: 2, respond: true, mode: modeServer}
	go s.serve(t)

	t.Cleanup(func() { conn.Close() })
	return s
}

func (s *fakeServer) serve(t *testing.T) {
	buf := make([]byte, 128)
	for {
		n, remote, err := s.conn.ReadFromUDP(buf)
		if err != nil {
			return // socket closed by test cleanup
		}
		if n < 48 {
			continue
		}

		s.mu.Lock()
		skew, stratum, leap, respond, shortLen := s.skew, s.stratum, s.leap, s.respond, s.shortLen
		s.mu.Unlock()

		if !respond {
			continue
		}

		var req packet
		copy(req[:], buf[:48])

		s.mu.Lock()
		mode, wrongOrigin := s.mode, s.wrongOrigin
		s.mu.Unlock()
		if mode == 0 {
			mode = modeServer
		}

		t2 := time.Now().UTC().Add(skew)
		var resp packet
		resp[0] = (4 << 3) | mode
		resp[0] |= leap << 6
		resp[1] = stratum
		origin := req.transmitTimestamp()
		if wrongOrigin {
			origin ^= 0xff // guaranteed to disagree with what the client sent
		}
		binary.BigEndian.PutUint64(resp[24:32], origin)    // echo origin (or not)
		binary.BigEndian.PutUint64(resp[32:40], toNTP(t2)) // receive
		t3 := time.Now().UTC().Add(skew)
		binary.BigEndian.PutUint64(resp[40:48], toNTP(t3)) // transmit

		out := resp[:]
		if shortLen > 0 && shortLen < len(out) {
			out = out[:shortLen]
		}
		if _, err := s.conn.WriteToUDP(out, remote); err != nil {
			return
		}
	}
}

func (s *fakeServer) setStratum(v byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stratum = v
}

func (s *fakeServer) setLeapUnsync() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.leap = leapUnsync
}

func (s *fakeServer) setSilent() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.respond = false
}

func (s *fakeServer) setShortReply(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shortLen = n
}

func (s *fakeServer) setMode(m byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mode = m
}

func (s *fakeServer) setWrongOrigin() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrongOrigin = true
}
