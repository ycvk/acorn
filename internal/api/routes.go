package api

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
)

func NewHandler(deps Dependencies) (http.Handler, error) {
	if deps.Threads == nil {
		return nil, errors.New("web thread service is required")
	}
	if deps.Runs == nil {
		return nil, errors.New("web run service is required")
	}
	if deps.Events == nil {
		return nil, errors.New("web event service is required")
	}
	if deps.PendingAction == nil {
		return nil, errors.New("web pending action service is required")
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
	if deps.Knowledge == nil {
		return nil, errors.New("web knowledge service is required")
	}
	if deps.Captures == nil {
		return nil, errors.New("web capture service is required")
	}

	if deps.Now == nil {
		return nil, errors.New("web now service is required")
	}
	if deps.PhoneNotifications == nil {
		return nil, errors.New("phone notification service is required")
	}
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}

	server := &Server{
		threads:            deps.Threads,
		runs:               deps.Runs,
		events:             deps.Events,
		pendingAction:      deps.PendingAction,
		skills:             deps.Skills,
		capabilities:       deps.Capabilities,
		deviceAuth:         deps.DeviceAuth,
		inbox:              deps.Inbox,
		knowledge:          deps.Knowledge,
		captures:           deps.Captures,
		phoneNotifications: deps.PhoneNotifications,
		now:                deps.Now,
		logger:             logger,
		cfg:                deps.Config,
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
			r.Put("/devices/self/push-token", s.handleSetPushToken)
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
				})
			})
			r.Post("/runs/{run_id}:interrupt", s.handleClientInterruptRun)
			r.Route("/runs/{run_id}", func(r chi.Router) {
				r.Get("/", s.handleClientGetRun)
				r.Get("/events", s.handleRunEvents)
				r.Get("/detail", s.handleClientRunDetail)
			})
			r.Get("/pending-actions", s.handleListPendingActions)
			r.Get("/pending-actions/{action_id}", s.handleGetPendingAction)
			r.Post("/pending-actions/{action_id}:decide", s.handleDecidePendingAction)
			r.Get("/inbox", s.handleClientInbox)
			r.Get("/now", s.handleGetNow)
			r.Post("/commitments/{commitment_id}:cancel", s.handleCancelCommitment)
			r.Post("/watches/{watch_id}:pause", s.handlePauseWatch)
			r.Post("/watches/{watch_id}:resume", s.handleResumeWatch)
			r.Post("/phone-notifications", s.handlePhoneNotifications)
			r.Post("/captures", s.handleCreateCapture)
			r.Get("/knowledge/notes", s.handleListKnowledgeNotes)
			r.Get("/knowledge/note", s.handleGetKnowledgeNote)
			r.Get("/knowledge/attachment", s.handleGetKnowledgeAttachment)
			r.Get("/system/status", s.handleClientSystemStatus)
			r.Get("/tools", s.handleClientTools)
			r.Route("/skills", func(r chi.Router) {
				r.Get("/", s.handleListSkills)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", s.handleGetSkill)
					r.Get("/files", s.handleReadSkillFile)
				})
			})
		})
	})
}
