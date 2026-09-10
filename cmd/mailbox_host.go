package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// Mailbox connection targets are operator-supplied strings, and the server is
// the one that connects to them. Without a policy an authenticated user with
// mailbox settings rights can point a mailbox at the loopback interface, at a
// link-local address, or at a cloud metadata endpoint, and then distinguish
// internal services from each other through the connection or authentication
// error the endpoint reports back. The background scanner would also keep
// connecting periodically.
//
// Policy, chosen deliberately for this deployment: loopback, link-local, cloud
// metadata, multicast, unspecified and broadcast addresses are refused. RFC1918
// and other private ranges stay allowed, because self-hosted mail servers on a
// private network are a supported setup and blocking them would break the
// primary use case.
//
// The check is applied to the resolved address and the validated address is the
// one that gets dialed, so a name that resolves differently on a second lookup
// (DNS rebinding) cannot move the connection to a blocked address.
var (
	// mailboxHostLookupTimeout bounds the name resolution that precedes a
	// connection attempt.
	mailboxHostLookupTimeout = 5 * time.Second

	// mailboxDialTimeout bounds the TCP connection attempt itself.
	mailboxDialTimeout = 10 * time.Second
)

// errMailboxHostBlocked reports a target that the mailbox host policy refuses.
var errMailboxHostBlocked = errors.New("mailbox host is not allowed")

// blockedMailboxRanges are the address ranges a mailbox may never connect to.
var blockedMailboxRanges = func() []*net.IPNet {
	cidrs := []string{
		"127.0.0.0/8",        // IPv4 loopback.
		"::1/128",            // IPv6 loopback.
		"169.254.0.0/16",     // IPv4 link-local, which is where cloud metadata lives.
		"fe80::/10",          // IPv6 link-local.
		"ff00::/8",           // IPv6 multicast.
		"224.0.0.0/4",        // IPv4 multicast.
		"0.0.0.0/32",         // Unspecified.
		"::/128",             // Unspecified.
		"255.255.255.255/32", // Broadcast.
	}

	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			// A malformed entry is a programming error, not a runtime condition.
			panic(fmt.Sprintf("invalid blocked mailbox range %q: %v", c, err))
		}
		out = append(out, n)
	}

	return out
}()

// metadataAddrs are cloud metadata endpoints that are refused even when they sit
// outside a blocked range above.
var metadataAddrs = []net.IP{
	net.ParseIP("169.254.169.254"), // AWS, GCP, Azure.
	net.ParseIP("100.100.100.200"), // Alibaba Cloud.
	net.ParseIP("192.0.0.192"),     // Oracle Cloud.
	net.ParseIP("fd00:ec2::254"),   // AWS over IPv6.
}

// checkMailboxIP reports why a resolved address may not be used as a mailbox
// target. A nil error means the address is allowed.
func checkMailboxIP(ip net.IP) error {
	if ip == nil {
		return fmt.Errorf("%w: could not determine the address", errMailboxHostBlocked)
	}
	for _, meta := range metadataAddrs {
		if meta.Equal(ip) {
			return fmt.Errorf("%w: %s is a cloud metadata endpoint", errMailboxHostBlocked, ip)
		}
	}
	for _, n := range blockedMailboxRanges {
		if n.Contains(ip) {
			return fmt.Errorf("%w: %s is a reserved address (%s)", errMailboxHostBlocked, ip, n)
		}
	}

	return nil
}

// mailboxHostPolicyCheck is the address policy applied to every mailbox
// connection. It is a variable so tests that stand up a fake POP server on the
// loopback interface can relax it: the policy deliberately refuses loopback.
var mailboxHostPolicyCheck = checkMailboxIP

// resolveMailboxHost resolves host and returns one address that the policy
// allows. When a name resolves to several addresses the first allowed one is
// used, so a name that also maps to a blocked address cannot make the server
// connect to it.
func resolveMailboxHost(ctx context.Context, host string) (net.IP, error) {
	host = trimMailboxHost(host)
	if host == "" {
		return nil, fmt.Errorf("%w: empty host", errMailboxHostBlocked)
	}

	// A literal address needs no lookup.
	if ip := net.ParseIP(host); ip != nil {
		if err := mailboxHostPolicyCheck(ip); err != nil {
			return nil, err
		}
		return ip, nil
	}

	ctx, cancel := context.WithTimeout(ctx, mailboxHostLookupTimeout)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("could not resolve mailbox host %q: %w", host, err)
	}

	var blocked error
	for _, ip := range ips {
		if err := mailboxHostPolicyCheck(ip); err == nil {
			return ip, nil
		} else if blocked == nil {
			blocked = err
		}
	}
	if blocked == nil {
		return nil, fmt.Errorf("%w: %q resolved to no address", errMailboxHostBlocked, host)
	}

	return nil, blocked
}

// trimMailboxHost normalizes an operator-supplied host the same way the POP3
// options do, so the policy sees exactly what would be dialed.
func trimMailboxHost(host string) string {
	host = strings.TrimSpace(host)
	// Tolerate a bracketed IPv6 literal and a trailing dot.
	if len(host) > 1 && host[0] == '[' && host[len(host)-1] == ']' {
		host = host[1 : len(host)-1]
	}
	for len(host) > 1 && host[len(host)-1] == '.' {
		host = host[:len(host)-1]
	}

	return host
}

// mailboxDialFunc establishes the TCP connection once the target address has
// been validated. It is a variable so tests can assert which address is dialed
// without touching the network.
var mailboxDialFunc = func(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
	dialer := &net.Dialer{}
	if timeout > 0 {
		dialer.Timeout = timeout
	} else {
		dialer.Timeout = mailboxDialTimeout
	}

	return dialer.DialContext(ctx, network, address)
}

// dialMailbox resolves, validates and connects to a mailbox target. The
// validated address is dialed directly, so the policy cannot be bypassed by a
// second, different resolution.
func dialMailbox(ctx context.Context, network, address string, dialTimeout time.Duration) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ip, err := resolveMailboxHost(ctx, host)
	if err != nil {
		return nil, err
	}

	return mailboxDialFunc(ctx, network, net.JoinHostPort(ip.String(), port), dialTimeout)
}

// mailboxPolicyDialer hands the POP3 client a policy-checked connection. The
// library keeps the hostname for TLS server-name verification, so pinning the
// address here does not weaken certificate checks.
type mailboxPolicyDialer struct{}

func (d *mailboxPolicyDialer) Dial(network, address string) (net.Conn, error) {
	return dialMailbox(context.Background(), network, address, mailboxDialTimeout)
}
