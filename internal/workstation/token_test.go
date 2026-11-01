package workstation

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestServerRefusesRequestsFromItsOwnAddresses(t *testing.T) {
	newHandler := func(refuseLocal bool) http.Handler {
		server, err := NewServer(ServerConfig{Home: t.TempDir(), Token: testToken, RefuseLocal: refuseLocal, Supervisor: NewSupervisor(io.Discard)})
		if err != nil {
			t.Fatal(err)
		}
		return server.Handler()
	}
	status := func(handler http.Handler, remote string) int {
		req := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
		req.RemoteAddr = remote
		req.Header.Set("Authorization", "Bearer "+testToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}
	refusing := newHandler(true)
	local := []string{"127.0.0.1:41000", "[::1]:41000"}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		t.Fatal(err)
	}
	for _, addr := range addrs {
		if n, ok := addr.(*net.IPNet); ok && !n.IP.IsLoopback() {
			local = append(local, net.JoinHostPort(n.IP.String(), "41000"))
		}
	}
	for _, remote := range local {
		if got := status(refusing, remote); got != http.StatusForbidden {
			t.Fatalf("request from %s: status %d, want 403", remote, got)
		}
	}
	if got := status(refusing, "192.0.2.1:41000"); got != http.StatusOK {
		t.Fatalf("request from the backend's address: status %d, want 200", got)
	}
	if got := status(newHandler(false), "127.0.0.1:41000"); got != http.StatusOK {
		t.Fatalf("a workstation beside the backend refused a local request: status %d", got)
	}
}

func TestCommandsDoNotInheritTheToken(t *testing.T) {
	t.Setenv(TokenEnv, testToken)
	token, err := TakeToken()
	if err != nil || token != testToken {
		t.Fatalf("token = %q, %v", token, err)
	}
	if _, ok := os.LookupEnv(TokenEnv); ok {
		t.Fatalf("%s is still in the environment", TokenEnv)
	}
	client, _ := newTestClient(t)
	got, err := client.Exec(t.Context(), ExecRequest{Command: "printenv " + TokenEnv + " || echo absent", TimeoutSeconds: 10, MaxOutputBytes: 1000})
	if err != nil || got.Stdout != "absent\n" {
		t.Fatalf("a command saw the token: %+v, %v", got, err)
	}
}
