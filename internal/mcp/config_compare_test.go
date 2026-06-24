package mcp

import "testing"

func TestProviderConfigEquivalentAuthDiff(t *testing.T) {
	base := ProviderConfig{
		Name:                  "test",
		Enabled:               true,
		Transport:             "stdio",
		Command:               "echo",
		StartupTimeoutSeconds: 10,
	}

	tests := []struct {
		name    string
		authA   AuthConfig
		authB   AuthConfig
		wantEql bool
	}{
		{
			name:    "same_auth",
			authA:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read"}},
			authB:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read"}},
			wantEql: true,
		},
		{
			name:    "different_auth_type",
			authA:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read"}},
			authB:   AuthConfig{Type: "api_key", ClientID: "client1", Scopes: []string{"read"}},
			wantEql: false,
		},
		{
			name:    "different_client_id",
			authA:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read"}},
			authB:   AuthConfig{Type: "oauth", ClientID: "client2", Scopes: []string{"read"}},
			wantEql: false,
		},
		{
			name:    "different_scopes_length",
			authA:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read"}},
			authB:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read", "write"}},
			wantEql: false,
		},
		{
			name:    "different_scopes_value",
			authA:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"read"}},
			authB:   AuthConfig{Type: "oauth", ClientID: "client1", Scopes: []string{"write"}},
			wantEql: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := base
			a.Auth = tt.authA
			b := base
			b.Auth = tt.authB
			got := providerConfigEquivalent(a, b)
			if got != tt.wantEql {
				t.Fatalf("providerConfigEquivalent = %v, want %v", got, tt.wantEql)
			}
		})
	}
}
