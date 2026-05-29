package web

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func NewHandler(deps Dependencies) (http.Handler, error) {
	if deps.Client == nil {
		return nil, errors.New("web client service is required")
	}
	if deps.PendingAction == nil {
		return nil, errors.New("web pending action service is required")
	}
	if deps.Checkpoints == nil {
		return nil, errors.New("web working checkpoint service is required")
	}
	if deps.Trace == nil {
		return nil, errors.New("web trace service is required")
	}
	if deps.Memory == nil {
		return nil, errors.New("web memory service is required")
	}
	if deps.Skills == nil {
		return nil, errors.New("web skill service is required")
	}
	if deps.Capabilities == nil {
		return nil, errors.New("web capabilities service is required")
	}
	if deps.DeviceAuth == nil {
		return nil, errors.New("web device auth service is required")
	}
	if deps.Inbox == nil {
		return nil, errors.New("web inbox service is required")
	}
	if deps.Notifications == nil {
		return nil, errors.New("web notification service is required")
	}

	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	server := &Server{
		client:        deps.Client,
		pendingAction: deps.PendingAction,
		checkpoints:   deps.Checkpoints,
		trace:         deps.Trace,
		memory:        deps.Memory,
		skills:        deps.Skills,
		capabilities:  deps.Capabilities,
		deviceAuth:    deps.DeviceAuth,
		inbox:         deps.Inbox,
		notifications: deps.Notifications,
		logger:        logger,
		cfg:           deps.Config,
	}

	router := chi.NewRouter()
	if server.cfg != nil && len(server.cfg.Web.AllowedOrigins) > 0 {
		router.Use(cors.Handler(cors.Options{
			AllowedOrigins:   server.cfg.Web.AllowedOrigins,
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
			AllowCredentials: true,
			MaxAge:           300,
		}))
	}
	router.Use(middleware.RequestID)
	router.Use(middleware.Recoverer)
	server.registerRoutes(router)
	return router, nil
}

func (s *Server) registerRoutes(router chi.Router) {
	router.Get("/healthz", s.handleHealthz)
	router.Route("/v1", func(r chi.Router) {
		r.Post("/devices:pair", s.handlePairDevice)
		r.Group(func(r chi.Router) {
			r.Use(s.requireDeviceAuth)
			r.Get("/devices", s.handleListDevices)
			r.Delete("/devices/{device_id}", s.handleRevokeDevice)
			r.Put("/devices/{device_id}/push-token", s.handleRegisterDevicePushToken)
			r.Delete("/devices/{device_id}/push-token/{provider}", s.handleRevokeDevicePushToken)
			r.Route("/threads", func(r chi.Router) {
				r.Get("/", s.handleClientListThreads)
				r.Post("/", s.handleClientCreateThread)
				r.Route("/{thread_id}", func(r chi.Router) {
					r.Get("/", s.handleClientGetThread)
					r.Patch("/", s.handleClientUpdateThread)
					r.Delete("/", s.handleClientDeleteThread)
					r.Get("/messages", s.handleClientListMessages)
					r.Post("/messages", s.handleClientCreateMessage)
					r.Post("/runs", s.handleClientCreateRun)
					r.Get("/checkpoint", s.handleGetWorkingCheckpoint)
					r.Put("/checkpoint", s.handleUpdateWorkingCheckpoint)
					r.Delete("/checkpoint", s.handleDeleteWorkingCheckpoint)
				})
			})
			r.Route("/runs/{run_id}", func(r chi.Router) {
				r.Get("/", s.handleClientGetRun)
				r.Get("/events", s.handleRunEvents)
				r.Get("/detail", s.handleClientRunDetail)
			})
			r.Post("/runs/{run_id}:interrupt", s.handleClientInterruptRun)
			r.Post("/runs/{run_id}:resume", s.handleClientResumeRun)
			r.Get("/pending-actions", s.handleListPendingActions)
			r.Get("/pending-actions/{action_id}", s.handleGetPendingAction)
			r.Post("/pending-actions/{action_id}:decide", s.handleDecidePendingAction)
			r.Get("/inbox", s.handleClientInbox)
			r.Get("/system/status", s.handleClientSystemStatus)
			r.Get("/tools", s.handleClientTools)
			r.Route("/memory", func(r chi.Router) {
				r.Get("/facts", s.handleListMemoryFacts)
				r.Get("/skills", s.handleListMemorySkills)
				r.Get("/history", s.handleListMemoryHistory)
				r.Get("/search", s.handleSearchMemory)
			})
			r.Route("/skills", func(r chi.Router) {
				r.Get("/", s.handleListSkills)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetSkill)
					r.Get("/files", s.handleReadSkillFile)
				})
			})
			r.Get("/settings", s.handleClientSettings)
			r.Patch("/settings", s.handlePatchClientSettings)
		})
	})
}
