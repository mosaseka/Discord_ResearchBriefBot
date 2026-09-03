package security

import (
	"context"
	"net"
	"testing"
)

type resolver struct{ addresses []net.IPAddr }

func (r resolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, nil
}

type changingResolver struct{ calls int }

func (r *changingResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	r.calls++
	if r.calls == 1 {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
}

func TestRejectsPrivateAndMixedDNS(t *testing.T) {
	for _, addresses := range [][]net.IPAddr{{{IP: net.ParseIP("127.0.0.1")}}, {{IP: net.ParseIP("93.184.216.34")}, {IP: net.ParseIP("10.0.0.1")}}} {
		policy := NewPolicy(resolver{addresses: addresses}, nil)
		if _, err := policy.ValidateURL(context.Background(), "https://example.org/x"); err == nil {
			t.Fatalf("accepted %v", addresses)
		}
	}
}
func TestAcceptsPublicHTTPS(t *testing.T) {
	policy := NewPolicy(resolver{addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}}, nil)
	if _, err := policy.ValidateURL(context.Background(), "https://example.org/x"); err != nil {
		t.Fatal(err)
	}
}
func TestRejectsHTTP(t *testing.T) {
	policy := NewPolicy(resolver{addresses: []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}}, nil)
	if _, err := policy.ValidateURL(context.Background(), "http://example.org/x"); err == nil {
		t.Fatal("expected rejection")
	}
}

func TestDNSRebindingIsRejectedAtDial(t *testing.T) {
	resolver := &changingResolver{}
	policy := NewPolicy(resolver, nil)
	if _, err := policy.ValidateURL(context.Background(), "https://example.org/x"); err != nil {
		t.Fatal(err)
	}
	if _, err := policy.DialContext(context.Background(), "tcp", "example.org:443"); err == nil {
		t.Fatal("dial accepted rebound private address")
	}
}
