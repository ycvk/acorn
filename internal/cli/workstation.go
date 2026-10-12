package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/ycvk/acorn/internal/workstation"
)

// runWorkstation serves the workstation API. It runs inside the workstation
// container and needs no Acorn config, only the shared token.
func runWorkstation(ctx context.Context, args []string) error {
	if len(args) == 0 || args[0] != "serve" {
		return errors.New("usage: acorn workstation serve [--listen addr] [--home dir]")
	}
	fs := newFlagSet("workstation serve")
	listen := fs.String("listen", "0.0.0.0:7810", "listen address")
	home := fs.String("home", os.Getenv("HOME"), "home directory for relative paths and commands")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	server, err := workstation.NewServer(*home, os.Getenv(workstation.TokenEnv))
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
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			slog.Error("workstation shutdown error", "error", err)
		}
	}()
	fmt.Printf("Acorn workstation listening on %s, home %s\n", *listen, *home)
	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
