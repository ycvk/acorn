package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/ycvk/acorn/internal/workstation"
)

// runWorkstation serves the workstation API. It runs inside the workstation
// container and needs no Acorn config, only the shared token. Companions are
// the long-running processes it keeps alive, such as the browser; the skill
// packages' MCP servers run under the same supervisor.
func runWorkstation(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("usage: acorn workstation serve [--listen addr] [--home dir] [--chromium path] [--devtools url] [--keep name=command]")
	}
	fs := newFlagSet("workstation serve")
	listen := fs.String("listen", "0.0.0.0:7810", "listen address")
	home := fs.String("home", os.Getenv("HOME"), "home directory for relative paths and commands")
	chromium := fs.String("chromium", "", "browser executable that renders pages; empty turns render off")
	devtools := fs.String("devtools", "", "DevTools URL of the agent's browser, for the owner's view; empty turns the view off")
	var companions []workstation.Companion
	fs.Func("keep", "keep a process running, as name=command; repeatable", func(spec string) error {
		c, err := workstation.ParseCompanion(spec)
		if err != nil {
			return err
		}
		companions = append(companions, c)
		return nil
	})
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	var view *workstation.View
	if *devtools != "" {
		var err error
		if view, err = workstation.NewView(*devtools, &http.Client{Timeout: 10 * time.Second}); err != nil {
			return err
		}
	}
	supervisor := workstation.NewSupervisor(os.Stdout)
	server, err := workstation.NewServer(workstation.ServerConfig{Home: *home, Token: os.Getenv(workstation.TokenEnv), Chromium: *chromium, View: view, Supervisor: supervisor})
	if err != nil {
		return err
	}
	httpServer := &http.Server{
		Addr:              *listen,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
	var companionsDone sync.WaitGroup
	companionsDone.Go(func() { supervisor.Run(ctx, companions) })
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("workstation shutdown error", "error", err)
		}
	}()
	fmt.Printf("Acorn workstation listening on %s, home %s, %d companions\n", *listen, *home, len(companions))
	err = httpServer.ListenAndServe()
	server.Close()
	companionsDone.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
