package web

import (
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/app"
)

func (s *Server) handleClientInbox(w http.ResponseWriter, r *http.Request) {
	inbox, err := s.inbox.Load(r.Context())
	if err != nil {
		s.respondClientKnownError(w, r, err)
		return
	}
	if inbox == nil {
		s.respondInternalError(w, r, errors.New("inbox service returned nil"))
		return
	}
	s.respondJSON(w, r, http.StatusOK, inboxDTOFromDomain(*inbox, clientWorkspaceRoot(s.cfg)))
}

func (s *Server) handleClientSystemStatus(w http.ResponseWriter, r *http.Request) {
	snapshot := s.capabilities.Snapshot(r.Context(), app.CapabilitySnapshotOptions{
		ProbeMCP: r.URL.Query().Get("probe_mcp") == "1",
	})
	s.respondJSON(w, r, http.StatusOK, systemStatusDTOFromSnapshot(snapshot, clientWorkspaceRoot(s.cfg)))
}

func (s *Server) handleClientTools(w http.ResponseWriter, r *http.Request) {
	snapshot := s.capabilities.Snapshot(r.Context(), app.CapabilitySnapshotOptions{
		ProbeMCP: r.URL.Query().Get("probe_mcp") == "1",
	})
	items := capabilitiesToolsDTOFromSnapshot(snapshot.Tools)
	s.respondJSON(w, r, http.StatusOK, ToolListResponse{
		Items: items,
		Total: len(items),
	})
}

func (s *Server) handleClientSettings(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, r, http.StatusOK, clientSettingsDTOFromConfig(s.cfg))
}

func (s *Server) handlePatchClientSettings(w http.ResponseWriter, r *http.Request) {
	s.respondError(w, r, http.StatusNotImplemented, "settings_write_unsupported", "client settings write endpoint is not implemented")
}
