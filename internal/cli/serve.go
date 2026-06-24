package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/api"
	"github.com/ycvk/acorn/internal/wire"
)

func runServe(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	configPath := addConfigFlag(fs)
	listenAddr := fs.String("listen", "", "listen address override, for example 127.0.0.1:8080")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*configPath)
	if err != nil {
		return err
	}

	addr := strings.TrimSpace(cfg.Web.ListenAddr)
	if override := strings.TrimSpace(*listenAddr); override != "" {
		addr = override
	}

	container, err := wire.NewContainer(ctx, cfg)
	if err != nil {
		return err
	}
	defer container.Close()

	handler, err := api.NewHandler(api.Dependencies{
		Threads:       container.Threads(),
		Runs:          container.Runs(),
		Events:        container.Events(),
		PendingAction: container.PendingAction(),
		RunResume:     container.RunResume(),
		Memory:        container.Memory(),
		Skills:        container.Skills(),
		Capabilities:  container.Capabilities(),
		DeviceAuth:    container.DeviceAuth(),
		Inbox:         container.Inbox(),
		Config:        container.Config(),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	})
	if err != nil {
		return err
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "error", err)
		}
	}()

	// Surface execution readiness at startup so the owner is not surprised by a
	// mid-run execution_not_ready. serve intentionally stays up when not ready
	// (pairing/inbox/approvals remain usable), so this is a loud banner, not a stop.
	// Printed to stdout alongside the listen banner so `serve 2>/dev/null` cannot
	// hide a NOT-READY while the listen line implies everything is fine.
	fmt.Println(executionReadinessBanner(cfg.ValidateExecutionReady()))
	fmt.Printf("Acorn Web 服务监听于 http://%s\n", addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// executionReadinessBanner renders the startup readiness line. A nil error means
// the runtime can execute tasks; a non-nil error is a loud (non-fatal) warning that
// every run will be rejected with execution_not_ready until the reason is fixed.
func executionReadinessBanner(readyErr error) string {
	if readyErr != nil {
		return fmt.Sprintf("Execution: NOT READY — tasks will be rejected with execution_not_ready until fixed.\n  Reason: %s\n  Run 'acorn doctor' for detail.", readyErr)
	}
	return "Execution: ready"
}
