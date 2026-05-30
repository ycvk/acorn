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

	"github.com/go-chi/chi/v5"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/web"
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

	container, err := app.NewContainer(ctx, cfg)
	if err != nil {
		return err
	}
	defer container.Close()

	handler, err := web.NewHandler(web.Dependencies{
		Client:        container.Client(),
		PendingAction: container.PendingAction(),
		Checkpoints:   container.Checkpoints(),
		RunResume:     container.RunResume(),
		Memory:        container.Memory(),
		Skills:        container.Skills(),
		Capabilities:  container.Capabilities(),
		DeviceAuth:    container.DeviceAuth(),
		Inbox:         container.Inbox(),
		Notifications: container.Notifications(),
		Config:        container.Config(),
		Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})),
	})
	if err != nil {
		return err
	}

	// Mount MCP server on /mcp/* if configured (MCPSV-04, D-07, D-08)
	if mcpSrv := container.MCPServer(); mcpSrv != nil {
		streamableHandler := mcp.NewStreamableHTTPHandler(
			func(_ *http.Request) *mcp.Server { return mcpSrv },
			nil,
		)
		if mux, ok := handler.(*chi.Mux); ok {
			mux.Route("/mcp", func(r chi.Router) {
				r.Handle("/*", http.StripPrefix("/mcp", streamableHandler))
			})
		}
		fmt.Printf("MCP server exposed at http://%s/mcp/\n", addr)
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

	fmt.Printf("Acorn Web 服务监听于 http://%s\n", addr)
	err = server.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
