package metrics

import (
	"context"
	stderrors "errors"
	"fmt"
	"net"
	"testing"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/loopback"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv6"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

const (
	netstackNIC  tcpip.NICID = 1
	netstackPort             = 5252
	netstackAddr             = "device:5252"
)

type netstackPeer struct {
	name    string
	addr    tcpip.Address
	proto   tcpip.NetworkProtocolNumber
	ip      net.IP
	blocked bool
}

var netstackPeers = []netstackPeer{
	{"tailnet IPv4 peer 100.64.0.1", tcpip.AddrFrom4([4]byte{100, 64, 0, 1}), ipv4.ProtocolNumber, net.ParseIP("100.64.0.1"), false},
	{"tailnet IPv6 peer fd7a:115c:a1e0::1", tcpip.AddrFrom16([16]byte{0xfd, 0x7a, 0x11, 0x5c, 0xa1, 0xe0, 15: 1}), ipv6.ProtocolNumber, net.ParseIP("fd7a:115c:a1e0::1"), false},
	{"loopback IPv4 peer 127.0.0.1", tcpip.AddrFrom4([4]byte{127, 0, 0, 1}), ipv4.ProtocolNumber, net.ParseIP("127.0.0.1"), true},
	{"loopback IPv6 peer ::1", tcpip.AddrFrom16([16]byte{15: 1}), ipv6.ProtocolNumber, net.ParseIP("::1"), true},
}

// newNetstack builds an in-process gvisor stack with a loopback NIC that owns
// every test peer address. tsnet dials tailnet peers through the same gonet
// adapter, so connections from this stack are the *gonet.TCPConn type that
// guardedDial sees in a tsnet deployment.
func newNetstack(t *testing.T) *stack.Stack {
	t.Helper()
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, ipv6.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})
	t.Cleanup(func() {
		s.Close()
		s.Wait()
	})
	if err := s.CreateNIC(netstackNIC, loopback.New()); err != nil {
		t.Fatalf("CreateNIC: %v", err)
	}
	for _, p := range netstackPeers {
		pa := tcpip.ProtocolAddress{Protocol: p.proto, AddressWithPrefix: p.addr.WithPrefix()}
		if err := s.AddProtocolAddress(netstackNIC, pa, stack.AddressProperties{}); err != nil {
			t.Fatalf("AddProtocolAddress(%s): %v", p.addr, err)
		}
	}
	s.SetRouteTable([]tcpip.Route{
		{Destination: header.IPv4EmptySubnet, NIC: netstackNIC},
		{Destination: header.IPv6EmptySubnet, NIC: netstackNIC},
	})
	return s
}

// listenNetstack accepts one connection on the peer's port and hands it to the
// returned channel; a failed Accept closes the channel.
func listenNetstack(t *testing.T, s *stack.Stack, p netstackPeer) <-chan net.Conn {
	t.Helper()
	ln, err := gonet.ListenTCP(s, tcpip.FullAddress{NIC: netstackNIC, Addr: p.addr, Port: netstackPort}, p.proto)
	if err != nil {
		t.Fatalf("ListenTCP(%s): %v", p.addr, err)
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

// netstackDial dials p over s. It insists on the network/address guardedDial
// was called with, so a wrapper that rewrites or drops them fails the test.
func netstackDial(s *stack.Stack, p netstackPeer) dialFunc {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		if network != "tcp" || addr != netstackAddr {
			return nil, fmt.Errorf("dial got (%q, %q), want (%q, %q)", network, addr, "tcp", netstackAddr)
		}
		c, err := gonet.DialContextTCP(ctx, s, tcpip.FullAddress{NIC: netstackNIC, Addr: p.addr, Port: netstackPort}, p.proto)
		if err != nil {
			// Return an untyped nil: a nil *gonet.TCPConn in a net.Conn is non-nil.
			return nil, err
		}
		return c, nil
	}
}

// awaitAccepted returns the server side of the connection or fails the test.
func awaitAccepted(ctx context.Context, t *testing.T, accepted <-chan net.Conn) net.Conn {
	t.Helper()
	select {
	case c, ok := <-accepted:
		if !ok {
			t.Fatal("server side accept failed")
		}
		return c
	case <-ctx.Done():
		t.Fatal("server side never accepted the connection")
	}
	return nil
}

// TestGuardedDialNetstackPeer runs guardedDial against a real gvisor netstack
// connection, the conn type tsnet returns for tailnet peers. It pins that
// RemoteAddr() yields the peer (not nil) so the SSRF check neither fails closed
// for legitimate devices nor lets a blocked peer through.
func TestGuardedDialNetstackPeer(t *testing.T) {
	for _, p := range netstackPeers {
		t.Run(p.name, func(t *testing.T) {
			s := newNetstack(t)
			accepted := listenNetstack(t, s, p)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			conn, err := guardedDial(netstackDial(s, p))(ctx, "tcp", netstackAddr)

			if !p.blocked {
				if err != nil {
					t.Fatalf("guardedDial: unexpected error: %v", err)
				}
				defer func() { _ = conn.Close() }()
				remote, ok := conn.RemoteAddr().(*net.TCPAddr)
				if !ok {
					t.Fatalf("RemoteAddr() = %#v, want *net.TCPAddr", conn.RemoteAddr())
				}
				if !remote.IP.Equal(p.ip) || remote.Port != netstackPort {
					t.Fatalf("RemoteAddr() = %s, want %s", remote, net.JoinHostPort(p.ip.String(), "5252"))
				}
				_ = awaitAccepted(ctx, t, accepted).Close()
				return
			}

			if !stderrors.Is(err, errForbiddenTarget) {
				t.Fatalf("guardedDial: err = %v, want errForbiddenTarget", err)
			}
			if conn != nil {
				t.Fatalf("guardedDial: conn = %v, want nil", conn)
			}
			srv := awaitAccepted(ctx, t, accepted)
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
}
