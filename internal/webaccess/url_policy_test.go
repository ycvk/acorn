package webaccess

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
)

type fakeResolver map[string][]string

func (f fakeResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	values, ok := f[host]
	if !ok {
		return nil, errors.New("not found")
	}
	out := make([]net.IPAddr, 0, len(values))
	for _, value := range values {
		out = append(out, net.IPAddr{IP: net.ParseIP(value)})
	}
	return out, nil
}

func TestURLPolicyAllowsPublicHTTPSTargets(t *testing.T) {
	policy := URLPolicy{Resolver: fakeResolver{"example.com": {"93.184.216.34"}}}
	got, err := policy.Validate(context.Background(), "https://example.com/docs")
	if err != nil {
		t.Fatalf("Validate public URL: %v", err)
	}
	if got.Scheme != "https" || got.Host != "example.com" || len(got.IPs) != 1 || got.IPs[0] != "93.184.216.34" {
		t.Fatalf("validated URL = %+v", got)
	}
}

func TestURLPolicyRejectsUnsupportedSchemesAndUserinfo(t *testing.T) {
	policy := URLPolicy{Resolver: fakeResolver{"example.com": {"93.184.216.34"}}}
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"file", "file:///etc/passwd", "unsupported url scheme"},
		{"ftp", "ftp://example.com/file", "unsupported url scheme"},
		{"userinfo", "https://user:pass@example.com", "userinfo"},
		{"missing host", "https:///path", "url host is required"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := policy.Validate(context.Background(), tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate(%q) error = %v, want %q", tc.raw, err, tc.want)
			}
		})
	}
}

func TestURLPolicyRejectsBlockedLiteralAddresses(t *testing.T) {
	policy := URLPolicy{}
	tests := []struct {
		raw  string
		want string
	}{
		{"http://localhost:8080", "localhost"},
		{"http://service.localhost", "localhost"},
		{"http://127.0.0.1", "loopback_address"},
		{"http://[::1]", "loopback_address"},
		{"http://0.0.0.0", "unspecified_address"},
		{"http://10.0.0.1", "private_address"},
		{"http://192.168.1.20", "private_address"},
		{"http://172.16.0.1", "private_address"},
		{"http://[fd00::1]", "private_address"},
		{"http://169.254.169.254", "metadata_address"},
		{"http://169.254.1.2", "link_local_address"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			_, err := policy.Validate(context.Background(), tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate(%q) error = %v, want %q", tc.raw, err, tc.want)
			}
		})
	}
}

func TestURLPolicyRejectsBlockedDNSResults(t *testing.T) {
	policy := URLPolicy{Resolver: fakeResolver{
		"internal.example": {"10.2.3.4"},
		"metadata.example": {"169.254.169.254"},
	}}
	tests := []struct {
		raw  string
		want string
	}{
		{"https://internal.example", "private_address"},
		{"https://metadata.example", "metadata_address"},
	}
	for _, tc := range tests {
		t.Run(tc.raw, func(t *testing.T) {
			_, err := policy.Validate(context.Background(), tc.raw)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Validate(%q) error = %v, want %q", tc.raw, err, tc.want)
			}
		})
	}
}

func TestURLPolicyAllowsPrivateNetworksOnlyWhenExplicitlyConfigured(t *testing.T) {
	policy := URLPolicy{
		AllowPrivateNetworks: true,
		Resolver: fakeResolver{
			"internal.example": {"10.2.3.4"},
		},
	}
	if _, err := policy.Validate(context.Background(), "http://10.0.0.1"); err != nil {
		t.Fatalf("Validate private literal with opt-in: %v", err)
	}
	if _, err := policy.Validate(context.Background(), "https://internal.example"); err != nil {
		t.Fatalf("Validate private DNS with opt-in: %v", err)
	}
	for _, raw := range []string{"http://127.0.0.1", "http://169.254.1.2", "http://169.254.169.254"} {
		_, err := policy.Validate(context.Background(), raw)
		if err == nil {
			t.Fatalf("Validate(%q) should still reject loopback/link-local/metadata", raw)
		}
	}
}
