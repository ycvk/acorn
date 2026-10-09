package runtime

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// modelTransport limits silence while waiting for headers or stream bytes.
// The deadline moves with progress, so long active generations can complete.
type modelTransport struct {
	base http.RoundTripper
	idle time.Duration
}

func (t *modelTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.idle == 0 {
		return t.base.RoundTrip(req)
	}
	ctx, cancel := context.WithCancel(req.Context())
	body := &modelResponseBody{cancel: cancel, idle: t.idle}
	body.timer = time.AfterFunc(t.idle, cancel)
	response, err := t.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		body.stop()
		return nil, err
	}
	body.reader = response.Body
	body.touch()
	response.Body = body
	return response, nil
}

type modelResponseBody struct {
	reader io.ReadCloser
	cancel context.CancelFunc
	idle   time.Duration
	timer  *time.Timer
	mu     sync.Mutex
	closed bool
}

func (b *modelResponseBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if n > 0 {
		b.touch()
	}
	if err != nil {
		b.stop()
	}
	return n, err
}

func (b *modelResponseBody) touch() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.timer.Reset(b.idle)
	}
}

func (b *modelResponseBody) stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.closed {
		b.closed = true
		b.timer.Stop()
		b.cancel()
	}
}

func (b *modelResponseBody) Close() error {
	b.stop()
	return b.reader.Close()
}
