package workstation

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"slices"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/webaccess"
)

// renderProxy is the only way out for the browser that renders a watched
// page: it connects, for the page, its redirects and everything the page
// loads, only to addresses the URL policy allows, and remembers the hosts it
// refused.
type renderProxy struct {
	listener net.Listener
	server   *http.Server
	dial     webaccess.DialFunc
	forward  *httputil.ReverseProxy

	served chan error

	mu      sync.Mutex
	refused []string
	tunnels map[net.Conn]struct{}
}

func startRenderProxy(ctx context.Context, policy webaccess.URLPolicy, dial webaccess.DialFunc) (*renderProxy, error) {
	var config net.ListenConfig
	listener, err := config.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("render proxy: %w", err)
	}
	p := &renderProxy{listener: listener, dial: policy.Dial(dial), served: make(chan error, 1), tunnels: map[net.Conn]struct{}{}}
	p.forward = &httputil.ReverseProxy{
		Rewrite:   func(*httputil.ProxyRequest) {},
		Transport: &http.Transport{DialContext: p.checked, TLSHandshakeTimeout: 10 * time.Second},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			http.Error(w, err.Error(), http.StatusBadGateway)
		},
	}
	p.server = &http.Server{Handler: p, ReadHeaderTimeout: 10 * time.Second}
	go func() { p.served <- p.server.Serve(listener) }()
	return p, nil
}

// Addr is the proxy's address for the browser's --proxy-server.
func (p *renderProxy) Addr() string { return p.listener.Addr().String() }

// Refused lists the hosts the policy refused, in the order first asked.
func (p *renderProxy) Refused() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.refused)
}

// Close stops the proxy and the tunnels still open. It reports the proxy
// failing on its own as well: the browser could not go out after that.
func (p *renderProxy) Close() error {
	err := p.server.Close()
	if served := <-p.served; !errors.Is(served, http.ErrServerClosed) {
		err = errors.Join(err, fmt.Errorf("render proxy: %w", served))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for conn := range p.tunnels {
		err = errors.Join(err, conn.Close())
	}
	return err
}

func (p *renderProxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodConnect:
		p.tunnel(w, r)
	case r.URL.IsAbs():
		p.forward.ServeHTTP(w, r)
	default:
		http.Error(w, "the render proxy only forwards", http.StatusBadRequest)
	}
}

// tunnel connects a CONNECT request to its host and copies bytes both ways
// until either side closes.
func (p *renderProxy) tunnel(w http.ResponseWriter, r *http.Request) {
	upstream, err := p.checked(r.Context(), "tcp", r.Host)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		_ = upstream.Close()
		http.Error(w, "the render proxy cannot tunnel", http.StatusInternalServerError)
		return
	}
	client, buffered, err := hijacker.Hijack()
	if err != nil {
		_ = upstream.Close()
		return
	}
	p.track(client, upstream)
	if _, err := client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n")); err != nil {
		p.untrack(client, upstream)
		return
	}
	// Either side closing or failing ends the tunnel; the browser sees that
	// as its connection ending, so a copy's error goes no further.
	done := make(chan error, 2)
	pipe := func(dst net.Conn, src io.Reader) {
		_, err := io.Copy(dst, src)
		done <- err
	}
	go pipe(upstream, bufio.NewReader(buffered))
	go pipe(client, upstream)
	<-done
	p.untrack(client, upstream)
	<-done
}

// checked dials through the policy and remembers a host it refuses.
func (p *renderProxy) checked(ctx context.Context, network, address string) (net.Conn, error) {
	conn, err := p.dial(ctx, network, address)
	if errors.Is(err, webaccess.ErrBlocked) {
		host, _, splitErr := net.SplitHostPort(address)
		if splitErr != nil {
			host = address
		}
		p.mu.Lock()
		if !slices.Contains(p.refused, host) {
			p.refused = append(p.refused, host)
		}
		p.mu.Unlock()
	}
	return conn, err
}

func (p *renderProxy) track(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range conns {
		p.tunnels[conn] = struct{}{}
	}
}

func (p *renderProxy) untrack(conns ...net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
		delete(p.tunnels, conn)
	}
}
