package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

// TestCheckMailboxIP pins the address policy that constrains where a mailbox is
// allowed to connect. Loopback, link-local, cloud metadata, multicast,
// unspecified and broadcast addresses are refused; private (RFC1918) ranges stay
// allowed because self-hosted mail servers on a private network are supported.
func TestCheckMailboxIP(t *testing.T) {
	for _, tc := range []struct {
		addr    string
		blocked bool
		why     string
	}{
		{"127.0.0.1", true, "IPv4 loopback"},
		{"127.10.20.30", true, "loopback range"},
		{"::1", true, "IPv6 loopback"},
		{"169.254.1.1", true, "IPv4 link-local"},
		{"169.254.169.254", true, "AWS/GCP/Azure metadata"},
		{"100.100.100.200", true, "Alibaba metadata"},
		{"192.0.0.192", true, "Oracle metadata"},
		{"fd00:ec2::254", true, "AWS metadata over IPv6"},
		{"fe80::1", true, "IPv6 link-local"},
		{"224.0.0.1", true, "IPv4 multicast"},
		{"ff02::1", true, "IPv6 multicast"},
		{"0.0.0.0", true, "unspecified"},
		{"255.255.255.255", true, "broadcast"},
		{"8.8.8.8", false, "public"},
		{"203.0.113.10", false, "public documentation range"},
		{"10.1.2.3", false, "RFC1918 stays allowed for self-hosted mail"},
		{"172.16.5.5", false, "RFC1918 stays allowed"},
		{"192.168.1.10", false, "RFC1918 stays allowed"},
		{"2001:db8::1", false, "public IPv6 range"},
	} {
		err := checkMailboxIP(net.ParseIP(tc.addr))
		if tc.blocked && err == nil {
			t.Errorf("checkMailboxIP(%s) allowed %s", tc.addr, tc.why)
		}
		if tc.blocked && !errors.Is(err, errMailboxHostBlocked) {
			t.Errorf("checkMailboxIP(%s) error does not wrap errMailboxHostBlocked: %v", tc.addr, err)
		}
		if !tc.blocked && err != nil {
			t.Errorf("checkMailboxIP(%s) rejected %s: %v", tc.addr, tc.why, err)
		}
	}

	if err := checkMailboxIP(nil); !errors.Is(err, errMailboxHostBlocked) {
		t.Fatalf("expected a nil address to be blocked, got %v", err)
	}
}

// TestResolveMailboxHostRejectsLiteralBlockedAddr covers the literal-address
// path, which must not need a lookup to be refused.
func TestResolveMailboxHostRejectsLiteralBlockedAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1", "::1", "169.254.169.254", "[::1]", "localhost."} {
		if _, err := resolveMailboxHost(context.Background(), addr); err == nil {
			t.Errorf("expected %q to be rejected", addr)
		}
	}
}

// TestResolveMailboxHostRejectsNameResolvingToBlockedAddr covers the lookup
// path: a name that resolves only to loopback is refused even though the input
// is not an address.
func TestResolveMailboxHostRejectsNameResolvingToBlockedAddr(t *testing.T) {
	if _, err := resolveMailboxHost(context.Background(), "localhost"); !errors.Is(err, errMailboxHostBlocked) {
		t.Fatalf("expected localhost to be refused, got %v", err)
	}
}

// TestResolveMailboxHostAcceptsPublicLiteral covers the allowed path and that the
// validated address is what gets returned for dialing.
func TestResolveMailboxHostAcceptsPublicLiteral(t *testing.T) {
	ip, err := resolveMailboxHost(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("expected a public address to be allowed: %v", err)
	}
	if !ip.Equal(net.ParseIP("8.8.8.8")) {
		t.Fatalf("resolveMailboxHost returned %v", ip)
	}
}

// TestDialMailboxDialsValidatedAddr is the DNS-rebinding guard: the dialer has to
// connect to the address the policy validated, not to a fresh resolution of the
// original name.
func TestDialMailboxDialsValidatedAddr(t *testing.T) {
	var dialed string
	original := mailboxDialFunc
	mailboxDialFunc = func(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
		dialed = address
		return nil, errors.New("stop before the network")
	}
	defer func() { mailboxDialFunc = original }()

	// A literal public address needs no resolver, so the validated address is
	// exactly what must be dialed.
	_, _ = dialMailbox(context.Background(), "tcp", "8.8.8.8:995", 0)
	if dialed != "8.8.8.8:995" {
		t.Fatalf("dialed %q, want the validated address 8.8.8.8:995", dialed)
	}

	// A blocked address must never reach the dialer.
	dialed = ""
	if _, err := dialMailbox(context.Background(), "tcp", "169.254.169.254:995", 0); !errors.Is(err, errMailboxHostBlocked) {
		t.Fatalf("expected the metadata address to be refused, got %v", err)
	}
	if dialed != "" {
		t.Fatalf("a blocked address reached the dialer: %q", dialed)
	}
}
