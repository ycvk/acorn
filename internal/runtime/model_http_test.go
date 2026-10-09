package runtime

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/config"
)

func TestModelStreamIdleDeadlineTracksProgress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flush := w.(http.Flusher)
		for range 8 {
			_, _ = w.Write([]byte("data\n"))
			flush.Flush()
			select {
			case <-time.After(20 * time.Millisecond):
			case <-r.Context().Done():
				return
			}
		}
	}))
	defer server.Close()
	client := &http.Client{Transport: &modelTransport{base: http.DefaultTransport, idle: 100 * time.Millisecond}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil || len(data) != 40 {
		t.Fatalf("active stream interrupted: %q, %v", data, err)
	}
}

func TestModelStreamIdleDeadlineCancelsStall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("start"))
		w.(http.Flusher).Flush()
		<-r.Context().Done()
	}))
	defer server.Close()
	client := &http.Client{Transport: &modelTransport{base: http.DefaultTransport, idle: 40 * time.Millisecond}}
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if _, err = io.ReadAll(response.Body); err == nil {
		t.Fatal("stalled stream completed without an error")
	}
}

func TestRunDeadlineOptional(t *testing.T) {
	executor := &Executor{runRuntime: &RunnerFactory{deps: RuntimeDeps{Config: config.DefaultConfig()}}, controller: NewRunController()}
	ctx, cancel := executor.newManagedRunContext(context.Background(), "run_deadline")
	defer cancel()
	if _, ok := ctx.Deadline(); ok {
		t.Fatal("zero run timeout imposed a deadline")
	}
	cancel()
	if ctx.Err() != context.Canceled {
		t.Fatal("run cannot be cancelled")
	}
}
