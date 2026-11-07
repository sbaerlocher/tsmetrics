package metrics

import (
	"context"
	stderrors "errors"
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/loopback"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

const netstackNIC tcpip.NICID = 1

var (
	netstackTailnetPeer  = tcpip.AddrFrom4([4]byte{100, 64, 0, 1})
	netstackLoopbackPeer = tcpip.AddrFrom4([4]byte{127, 0, 0, 1})
)

// newNetstack builds an in-process gvisor stack with a loopback NIC that owns
// both test peer addresses. tsnet dials tailnet peers through the same gonet
// adapter, so connections from this stack are the *gonet.TCPConn type that
// guardedDial sees in a tsnet deployment.
func newNetstack(t *testing.T) *stack.Stack {
	t.Helper()
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})
	t.Cleanup(func() {
		s.Close()
		s.Wait()
	})
	if err := s.CreateNIC(netstackNIC, loopback.New()); err != nil {
		t.Fatalf("CreateNIC: %v", err)
	}
	for _, addr := range []tcpip.Address{netstackTailnetPeer, netstackLoopbackPeer} {
		pa := tcpip.ProtocolAddress{Protocol: ipv4.ProtocolNumber, AddressWithPrefix: addr.WithPrefix()}
		if err := s.AddProtocolAddress(netstackNIC, pa, stack.AddressProperties{}); err != nil {
			t.Fatalf("AddProtocolAddress(%s): %v", addr, err)
		}
	}
	s.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: netstackNIC}})
	return s
}

// listenNetstack accepts one connection on peer:5252 and hands it to the
// returned channel.
func listenNetstack(t *testing.T, s *stack.Stack, peer tcpip.Address) <-chan net.Conn {
	t.Helper()
	ln, err := gonet.ListenTCP(s, tcpip.FullAddress{NIC: netstackNIC, Addr: peer, Port: 5252}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatalf("ListenTCP(%s): %v", peer, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	return accepted
}

func netstackDial(s *stack.Stack, peer tcpip.Address) dialFunc {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		c, err := gonet.DialContextTCP(ctx, s, tcpip.FullAddress{NIC: netstackNIC, Addr: peer, Port: 5252}, ipv4.ProtocolNumber)
		if err != nil {
			// Return an untyped nil: a nil *gonet.TCPConn in a net.Conn is non-nil.
			return nil, err
		}
		return c, nil
	}
}

// TestGuardedDialNetstackPeer runs guardedDial against a real gvisor netstack
// connection, the conn type tsnet returns for tailnet peers. It pins that
// RemoteAddr() yields the peer (not nil) so the SSRF check neither fails closed
// for legitimate devices nor lets a blocked peer through.
func TestGuardedDialNetstackPeer(t *testing.T) {
	t.Run("tailnet peer 100.64.0.1", func(t *testing.T) {
		s := newNetstack(t)
		accepted := listenNetstack(t, s, netstackTailnetPeer)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := guardedDial(netstackDial(s, netstackTailnetPeer))(ctx, "tcp", "device:5252")
		if err != nil {
			t.Fatalf("guardedDial: unexpected error: %v", err)
		}
		defer func() { _ = conn.Close() }()

		remote, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			t.Fatalf("RemoteAddr() = %#v, want *net.TCPAddr", conn.RemoteAddr())
		}
		if !remote.IP.Equal(net.IPv4(100, 64, 0, 1)) || remote.Port != 5252 {
			t.Fatalf("RemoteAddr() = %s, want 100.64.0.1:5252", remote)
		}

		select {
		case srv, ok := <-accepted:
			if ok {
				_ = srv.Close()
			}
		case <-ctx.Done():
			t.Fatal("server side never accepted the connection")
		}
	})

	t.Run("loopback peer 127.0.0.1", func(t *testing.T) {
		s := newNetstack(t)
		accepted := listenNetstack(t, s, netstackLoopbackPeer)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		conn, err := guardedDial(netstackDial(s, netstackLoopbackPeer))(ctx, "tcp", "device:5252")
		if !stderrors.Is(err, errForbiddenTarget) {
			t.Fatalf("guardedDial: err = %v, want errForbiddenTarget", err)
		}
		if conn != nil {
			t.Fatalf("guardedDial: conn = %v, want nil", conn)
		}

		var srv net.Conn
		select {
		case c, ok := <-accepted:
			if !ok {
				t.Fatal("server side accept failed")
			}
			srv = c
		case <-ctx.Done():
			t.Fatal("server side never accepted the connection")
		}
		defer func() { _ = srv.Close() }()

		// guardedDial must have closed the rejected conn: the server reads EOF
		// (or an error), never blocks until the deadline.
		if err := srv.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
			t.Fatalf("SetReadDeadline: %v", err)
		}
		_, err = srv.Read(make([]byte, 1))
		if err == nil {
			t.Fatal("server read succeeded, want EOF from the closed client conn")
		}
		var netErr net.Error
		if stderrors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("server read timed out, client conn was not closed: %v", err)
		}
	})
}
