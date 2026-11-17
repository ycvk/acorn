package webaccess

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
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
	// 198.18.0.x is what a fake-IP DNS resolver answers for every name.
	policy := URLPolicy{Resolver: fakeResolver{"example.com": {"93.184.216.34"}, "proxied.example": {"198.18.0.187"}, "wrapped.example": {"64:ff9b::5db8:d822"}}}
	for _, raw := range []string{"https://proxied.example", "https://wrapped.example", "http://198.18.0.1"} {
		if _, err := policy.Validate(context.Background(), raw); err != nil {
			t.Fatalf("Validate(%q): %v", raw, err)
		}
	}
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
		{"http://100.64.0.1", "private_address"},
		{"http://100.127.255.254", "private_address"},
		{"http://100.100.100.200", "metadata_address"},
		{"http://0.1.2.3", "unspecified_address"},
		{"http://[64:ff9b::a00:1]", "private_address"},
		{"http://[64:ff9b::7f00:1]", "loopback_address"},
		{"http://[64:ff9b::a9fe:a9fe]", "metadata_address"},
		{"http://[64:ff9b::6440:1]", "private_address"},
		{"http://[2002:a00:1::1]", "private_address"},
		{"http://[2002:7f00:1::1]", "loopback_address"},
		{"http://[2002:a9fe:a9fe::1]", "metadata_address"},
		{"http://[64:ff9b:1::a00:1]", "tunneled_address"},
		{"http://[64:ff9b:1:a00:0:100::]", "tunneled_address"},
		{"http://[2001:0:4136:e378:8000:63bf:80ff:fffe]", "tunneled_address"},
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
		"cgnat.example":    {"100.64.0.7"},
		"alibaba.example":  {"100.100.100.200"},
		"nat64.example":    {"64:ff9b::a00:1"},
	}}
	tests := []struct {
		raw  string
		want string
	}{
		{"https://internal.example", "private_address"},
		{"https://metadata.example", "metadata_address"},
		{"https://cgnat.example", "private_address"},
		{"https://alibaba.example", "metadata_address"},
		{"https://nat64.example", "private_address"},
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
	for _, raw := range []string{"http://10.0.0.1", "http://100.64.0.1", "http://[64:ff9b::a00:1]", "http://[2002:a00:1::1]"} {
		if _, err := policy.Validate(context.Background(), raw); err != nil {
			t.Fatalf("Validate(%q) with opt-in: %v", raw, err)
		}
	}
	if _, err := policy.Validate(context.Background(), "https://internal.example"); err != nil {
		t.Fatalf("Validate private DNS with opt-in: %v", err)
	}
	for _, raw := range []string{"http://127.0.0.1", "http://169.254.1.2", "http://169.254.169.254", "http://100.100.100.200", "http://0.1.2.3", "http://[64:ff9b::7f00:1]", "http://[2002:a9fe:a9fe::1]"} {
		_, err := policy.Validate(context.Background(), raw)
		if err == nil {
			t.Fatalf("Validate(%q) should still reject loopback/link-local/metadata", raw)
		}
	}
}

func TestURLPolicyReachesOnlyTheOriginOfATrustedBaseURL(t *testing.T) {
	policy := URLPolicy{Trusted: []string{"http://127.0.0.1:1200/", "https://rsshub.lan"}, Resolver: fakeResolver{"rsshub.lan": {"10.0.0.5"}}}
	for _, raw := range []string{"http://127.0.0.1:1200/zhihu/hot", "HTTP://127.0.0.1:1200", "https://rsshub.lan:443/github/trending/daily"} {
		if _, err := policy.Validate(context.Background(), raw); err != nil {
			t.Fatalf("Validate(%q) = %v", raw, err)
		}
	}
	for _, raw := range []string{"http://127.0.0.1:1201/", "https://127.0.0.1:1200/", "http://localhost:1200/", "http://rsshub.lan/", "http://user@127.0.0.1:1200/", "http://169.254.169.254/"} {
		if _, err := policy.Validate(context.Background(), raw); err == nil {
			t.Fatalf("Validate(%q) passed", raw)
		}
	}
	if _, err := (URLPolicy{Trusted: []string{"rsshub"}}).Validate(context.Background(), "http://127.0.0.1:1200/"); err == nil || !strings.Contains(err.Error(), "trusted base url") {
		t.Fatalf("a relative trusted url err = %v", err)
	}
}

// rebinding answers a public address the first time a name is looked up and
// loopback after that, as a DNS rebinding attacker's server does.
type rebinding struct {
	mu      sync.Mutex
	lookups map[string]int
}

func (r *rebinding) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups[host]++
	if r.lookups[host] == 1 {
		return []net.IPAddr{{IP: net.ParseIP("93.184.216.34")}}, nil
	}
	return []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}, nil
}

func TestFetchChecksTheAddressItConnectsTo(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hop" {
			http.Redirect(w, r, "http://rebind.test:"+r.Host[strings.LastIndex(r.Host, ":")+1:]+"/inside", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("inside"))
	}))
	defer server.Close()
	port := server.URL[strings.LastIndex(server.URL, ":")+1:]
	fetcher := func(resolver Resolver) *FetchService {
		service, err := NewFetchService(FetchConfig{
			UserAgent:        "Acorn test",
			Timeout:          2 * time.Second,
			MaxResponseBytes: 1 << 20,
			Policy:           URLPolicy{Resolver: resolver, Trusted: []string{server.URL}},
		})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}

	got, err := fetcher(fakeResolver{}).FetchRaw(context.Background(), server.URL+"/trusted", nil)
	if err != nil || string(got.Body) != "inside" {
		t.Fatalf("trusted origin = %q, %v", got.Body, err)
	}
	for _, target := range []string{"http://rebind.test:" + port + "/inside", server.URL + "/hop"} {
		_, err := fetcher(&rebinding{lookups: map[string]int{}}).FetchRaw(context.Background(), target, nil)
		if err == nil || !strings.Contains(err.Error(), `blocked url host "rebind.test" resolved to 127.0.0.1: loopback_address`) {
			t.Fatalf("FetchRaw(%s) error = %v, want the loopback address refused", target, err)
		}
	}
}
