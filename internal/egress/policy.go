// Package egress implements the server's outbound host policy for database
// connections. Because the server dials user-supplied hosts on the operator's
// network, a malicious tenant could otherwise point a connection (or a
// TestConnection probe — which reports success/failure and is thus an oracle) at
// the cloud metadata endpoint, the server's own loopback, or other sensitive
// internal addresses. The policy vetoes such hosts before any dial.
//
// A nil *Policy allows everything, which is exactly what the desktop build wants
// (a local single user freely connects to localhost); only the server injects a
// restrictive policy, so the desktop path is unaffected.
package egress

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// defaultDenyCIDRs are always blocked under the recommended posture: loopback,
// link-local (which contains the 169.254.169.254 cloud-metadata endpoint),
// unspecified, and multicast — for both IPv4 and IPv6. Private/internal ranges
// (10/8, 172.16/12, 192.168/16, fc00::/7) are deliberately NOT blocked, since
// reaching internal databases is the server's whole purpose.
var defaultDenyCIDRs = []string{
	"127.0.0.0/8",    // IPv4 loopback
	"::1/128",        // IPv6 loopback
	"169.254.0.0/16", // IPv4 link-local (incl. cloud metadata)
	"fe80::/10",      // IPv6 link-local
	"0.0.0.0/8",      // IPv4 "this" network / unspecified
	"::/128",         // IPv6 unspecified
	"224.0.0.0/4",    // IPv4 multicast
	"ff00::/8",       // IPv6 multicast
	// AWS IPv6 IMDS lives inside the ULA range fc00::/7 that we otherwise allow
	// for internal databases, so deny the exact endpoint. Operators on other
	// clouds can add their IPv6 metadata address via AIDB_EGRESS_DENY_CIDR.
	"fd00:ec2::254/128",
}

// Policy is an immutable host-egress rule set. The zero value is not usable;
// build one with New. A nil *Policy allows every host.
type Policy struct {
	allow []*net.IPNet // when non-empty, every resolved IP must fall in one of these
	deny  []*net.IPNet // any resolved IP in these is rejected
}

// New builds a policy from operator-supplied CIDR (or bare-IP) lists. denyCIDRs
// are appended to the built-in special-range denies; allowCIDRs, when non-empty,
// turn on allowlist mode (a host must match one of them in addition to passing
// the denies). An empty/unparseable entry is an error so misconfiguration fails
// loudly at startup rather than silently opening a hole.
func New(allowCIDRs, denyCIDRs []string) (*Policy, error) {
	p := &Policy{}
	for _, cidr := range defaultDenyCIDRs {
		n, err := parseNet(cidr)
		if err != nil {
			return nil, fmt.Errorf("egress: built-in deny %q: %w", cidr, err)
		}
		p.deny = append(p.deny, n)
	}
	for _, cidr := range denyCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		n, err := parseNet(cidr)
		if err != nil {
			return nil, fmt.Errorf("egress: deny %q: %w", cidr, err)
		}
		p.deny = append(p.deny, n)
	}
	for _, cidr := range allowCIDRs {
		cidr = strings.TrimSpace(cidr)
		if cidr == "" {
			continue
		}
		n, err := parseNet(cidr)
		if err != nil {
			return nil, fmt.Errorf("egress: allow %q: %w", cidr, err)
		}
		p.allow = append(p.allow, n)
	}
	return p, nil
}

// parseNet accepts either a CIDR ("10.0.0.0/8") or a bare IP ("10.0.0.5"),
// normalising the latter to a single-host network.
func parseNet(s string) (*net.IPNet, error) {
	if strings.Contains(s, "/") {
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, err
		}
		return n, nil
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return nil, fmt.Errorf("not a valid IP or CIDR")
	}
	bits := 32
	if ip.To4() == nil {
		bits = 128
	}
	return &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)}, nil
}

// AllowHost resolves host (an IP literal or a DNS name) and asserts that EVERY
// resolved address passes the policy. A nil policy allows everything.
//
// Limitation (documented, accepted for this phase): resolve-then-dial has a
// TOCTOU window — a hostile DNS server could return a benign IP here and a
// blocked one when the driver actually dials (DNS rebinding). Closing it fully
// requires dialing the vetted IP through a custom resolver in every driver.
// Operators needing that guarantee should combine this policy with network-level
// egress controls and explicit AIDB_EGRESS_ALLOW_CIDR allowlists.
func (p *Policy) AllowHost(ctx context.Context, host string) error {
	if p == nil {
		return nil
	}
	host = strings.TrimSpace(host)
	if host == "" {
		return fmt.Errorf("egress: empty host")
	}
	ips, err := resolve(ctx, host)
	if err != nil {
		return fmt.Errorf("egress: resolve %q: %w", host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("egress: host %q did not resolve to any address", host)
	}
	for _, ip := range ips {
		if contains(p.deny, ip) {
			return fmt.Errorf("egress: host %q (%s) is blocked by policy", host, ip)
		}
		if len(p.allow) > 0 && !contains(p.allow, ip) {
			return fmt.Errorf("egress: host %q (%s) is not in the allowlist", host, ip)
		}
	}
	return nil
}

// resolve returns the addresses for host: a single IP for a literal, otherwise a
// DNS lookup of both A and AAAA records.
func resolve(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}
	return net.DefaultResolver.LookupIP(ctx, "ip", host)
}

// contains reports whether ip falls in any of nets, checking both the native and
// the IPv4-mapped form so ::ffff:127.0.0.1 is caught by 127.0.0.0/8.
func contains(nets []*net.IPNet, ip net.IP) bool {
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
		if v4 := ip.To4(); v4 != nil && n.Contains(v4) {
			return true
		}
	}
	return false
}
