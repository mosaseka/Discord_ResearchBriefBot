package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var ErrUnsafeURL = errors.New("unsafe URL")

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Policy struct {
	resolver Resolver
	dialer   Dialer
}

func NewPolicy(resolver Resolver, dialer Dialer) *Policy {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	if dialer == nil {
		dialer = &net.Dialer{}
	}
	return &Policy{resolver: resolver, dialer: dialer}
}

func (p *Policy) ValidateURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, unsafe("parse: %v", err)
	}
	if err := p.CheckURL(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

func (p *Policy) CheckURL(ctx context.Context, u *url.URL) error {
	if p == nil {
		p = NewPolicy(nil, nil)
	}
	if u == nil || u.Opaque != "" || !strings.EqualFold(u.Scheme, "https") || u.User != nil || u.Hostname() == "" {
		return unsafe("only absolute HTTPS URLs without userinfo are allowed")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.Contains(host, "%") {
		return unsafe("localhost and scoped addresses are not allowed")
	}
	_, err := p.lookupPublic(ctx, host)
	return err
}

func (p *Policy) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split dial address: %w", err)
	}
	addresses, err := p.lookupPublic(ctx, strings.TrimSuffix(strings.ToLower(host), "."))
	if err != nil {
		return nil, err
	}
	var joined error
	for _, ip := range addresses {
		conn, dialErr := p.dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		joined = errors.Join(joined, dialErr)
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("dial validated public addresses: %w", joined)
}

func (p *Policy) lookupPublic(ctx context.Context, host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		if !IsPublicIP(ip) {
			return nil, unsafe("address %s is not public", ip)
		}
		return []net.IP{ip}, nil
	}
	resolved, err := p.resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	if len(resolved) == 0 {
		return nil, unsafe("host resolved to no addresses")
	}
	out := make([]net.IP, 0, len(resolved))
	for _, value := range resolved {
		if !IsPublicIP(value.IP) {
			return nil, unsafe("host %q resolved to non-public address %s", host, value.IP)
		}
		out = append(out, value.IP)
	}
	return out, nil
}

func IsPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsValid() || !addr.IsGlobalUnicast() || addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsMulticast() || addr.IsUnspecified() {
		return false
	}
	for _, prefix := range denied {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

var denied = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"), netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), netip.MustParsePrefix("100::/64"), netip.MustParsePrefix("2001:db8::/32"),
}

func unsafe(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrUnsafeURL, fmt.Sprintf(format, args...))
}
