package webaccess

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

type Resolver interface {
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
}

type URLPolicy struct {
	AllowPrivateNetworks bool
	Resolver             Resolver
}

type ValidatedURL struct {
	Raw        string
	Normalized string
	Scheme     string
	Host       string
	IPs        []string
}

func (p URLPolicy) Validate(ctx context.Context, raw string) (ValidatedURL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ValidatedURL{}, errors.New("url is required")
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ValidatedURL{}, fmt.Errorf("parse url: %w", err)
	}
	switch strings.ToLower(strings.TrimSpace(parsed.Scheme)) {
	case "http", "https":
	default:
		return ValidatedURL{}, fmt.Errorf("unsupported url scheme %q", parsed.Scheme)
	}
	if parsed.User != nil {
		return ValidatedURL{}, errors.New("url userinfo is not allowed")
	}
	host := strings.TrimSpace(parsed.Hostname())
	if host == "" {
		return ValidatedURL{}, errors.New("url host is required")
	}
	if isLocalhostName(host) {
		return ValidatedURL{}, fmt.Errorf("blocked localhost host %q", host)
	}

	ips, err := p.validateHost(ctx, host)
	if err != nil {
		return ValidatedURL{}, err
	}
	out := make([]string, 0, len(ips))
	for _, ip := range ips {
		out = append(out, ip.String())
	}
	return ValidatedURL{
		Raw:        trimmed,
		Normalized: parsed.String(),
		Scheme:     strings.ToLower(parsed.Scheme),
		Host:       host,
		IPs:        out,
	}, nil
}

func (p URLPolicy) validateHost(ctx context.Context, host string) ([]netip.Addr, error) {
	if ip, err := netip.ParseAddr(host); err == nil {
		addr := ip.Unmap()
		if reason := blockedAddrReason(addr, p.AllowPrivateNetworks); reason != "" {
			return nil, fmt.Errorf("blocked url host %q: %s", host, reason)
		}
		return []netip.Addr{addr}, nil
	}

	resolver := p.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	resolved, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve url host %q: %w", host, err)
	}
	if len(resolved) == 0 {
		return nil, fmt.Errorf("resolve url host %q: no addresses", host)
	}
	addrs := make([]netip.Addr, 0, len(resolved))
	for _, item := range resolved {
		addr, ok := netip.AddrFromSlice(item.IP)
		if !ok {
			return nil, fmt.Errorf("resolve url host %q: invalid address %q", host, item.IP.String())
		}
		addr = addr.Unmap()
		if reason := blockedAddrReason(addr, p.AllowPrivateNetworks); reason != "" {
			return nil, fmt.Errorf("blocked url host %q resolved to %s: %s", host, addr.String(), reason)
		}
		addrs = append(addrs, addr)
	}
	return addrs, nil
}

func isLocalhostName(host string) bool {
	lower := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(host)), ".")
	return lower == "localhost" || strings.HasSuffix(lower, ".localhost")
}

func blockedAddrReason(addr netip.Addr, allowPrivate bool) string {
	if !addr.IsValid() {
		return "invalid_address"
	}
	if isMetadataAddr(addr) {
		return "metadata_address"
	}
	if addr.IsUnspecified() {
		return "unspecified_address"
	}
	if addr.IsLoopback() {
		return "loopback_address"
	}
	if addr.IsMulticast() {
		return "multicast_address"
	}
	if addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() {
		return "link_local_address"
	}
	if !allowPrivate && addr.IsPrivate() {
		return "private_address"
	}
	return ""
}

func isMetadataAddr(addr netip.Addr) bool {
	return addr == netip.MustParseAddr("169.254.169.254") ||
		addr == netip.MustParseAddr("fd00:ec2::254")
}
