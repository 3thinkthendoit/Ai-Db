package egress

import (
	"context"
	"testing"
)

func TestNilPolicyAllowsEverything(t *testing.T) {
	var p *Policy
	// The desktop build passes a nil policy: even loopback/metadata are allowed.
	for _, host := range []string{"127.0.0.1", "169.254.169.254", "::1", "10.0.0.5"} {
		if err := p.AllowHost(context.Background(), host); err != nil {
			t.Fatalf("nil policy must allow %q, got %v", host, err)
		}
	}
}

func TestDefaultPolicyBlocksSpecialsAllowsPrivate(t *testing.T) {
	p, err := New(nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()

	blocked := []string{
		"127.0.0.1",        // loopback
		"127.5.5.5",        // loopback /8
		"::1",              // loopback v6
		"169.254.169.254",  // cloud metadata
		"169.254.1.1",      // link-local
		"fe80::1",          // link-local v6
		"0.0.0.0",          // unspecified
		"224.0.0.1",        // multicast
		"::ffff:127.0.0.1", // IPv4-mapped loopback
		"fd00:ec2::254",    // AWS IPv6 IMDS (inside allowed ULA fc00::/7)
	}
	for _, host := range blocked {
		if err := p.AllowHost(ctx, host); err == nil {
			t.Errorf("expected %q to be blocked by default policy, got allow", host)
		}
	}

	allowed := []string{
		"10.0.0.5",     // private
		"192.168.1.20", // private
		"172.16.0.9",   // private
		"8.8.8.8",      // public
		"fc00::1",      // IPv6 ULA (private)
	}
	for _, host := range allowed {
		if err := p.AllowHost(ctx, host); err != nil {
			t.Errorf("expected %q to be allowed by default policy, got %v", host, err)
		}
	}
}

func TestEmptyHostRejected(t *testing.T) {
	p, _ := New(nil, nil)
	if err := p.AllowHost(context.Background(), "   "); err == nil {
		t.Fatal("expected empty host to be rejected")
	}
}

func TestOperatorDenyCIDR(t *testing.T) {
	// Operator additionally blocks an internal range that the default allows.
	p, err := New(nil, []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := p.AllowHost(ctx, "10.1.2.3"); err == nil {
		t.Error("expected operator-denied 10.1.2.3 to be blocked")
	}
	if err := p.AllowHost(ctx, "192.168.1.1"); err != nil {
		t.Errorf("expected 192.168.1.1 still allowed, got %v", err)
	}
}

func TestAllowlistMode(t *testing.T) {
	// Strict allowlist: only 10.0.0.0/24 is reachable; everything else is denied
	// even though it is a normal private/public address.
	p, err := New([]string{"10.0.0.0/24"}, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := p.AllowHost(ctx, "10.0.0.7"); err != nil {
		t.Errorf("expected allowlisted 10.0.0.7, got %v", err)
	}
	if err := p.AllowHost(ctx, "10.0.1.7"); err == nil {
		t.Error("expected 10.0.1.7 outside the /24 allowlist to be blocked")
	}
	if err := p.AllowHost(ctx, "8.8.8.8"); err == nil {
		t.Error("expected public 8.8.8.8 to be blocked in allowlist mode")
	}
	// Default denies still apply inside allowlist mode.
	if err := p.AllowHost(ctx, "169.254.169.254"); err == nil {
		t.Error("expected metadata to remain blocked in allowlist mode")
	}
}

func TestBareIPAndBadCIDR(t *testing.T) {
	// A bare IP in the deny list is normalised to a /32.
	p, err := New(nil, []string{"192.168.5.5"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ctx := context.Background()
	if err := p.AllowHost(ctx, "192.168.5.5"); err == nil {
		t.Error("expected bare-IP deny to block 192.168.5.5")
	}
	if err := p.AllowHost(ctx, "192.168.5.6"); err != nil {
		t.Errorf("expected neighbour 192.168.5.6 allowed, got %v", err)
	}

	if _, err := New(nil, []string{"not-a-cidr"}); err == nil {
		t.Error("expected an unparseable deny entry to fail New")
	}
}
