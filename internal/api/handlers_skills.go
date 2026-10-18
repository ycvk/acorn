package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	items, err := s.skills.Overview(r.Context())
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillListResponse{Items: skillSummaryDTOs(items)})
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	page, err := s.skills.Page(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillEnvelope{Item: skillDetailDTO(page)})
}

func (s *Server) handleListSkillRevisions(w http.ResponseWriter, r *http.Request) {
	revisions, err := s.skills.Revisions(r.Context(), chi.URLParam(r, "name"))
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, skillRevisionListResponse(revisions))
}

func (s *Server) handleGetSkillRevision(w http.ResponseWriter, r *http.Request) {
	revision, err := parseRevision(chi.URLParam(r, "revision"), "revision")
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	version, err := s.skills.Revision(r.Context(), chi.URLParam(r, "name"), revision)
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, skillRevisionDTO(version))
}

func (s *Server) handleSkillDiff(w http.ResponseWriter, r *http.Request) {
	from, err := parseRevision(r.URL.Query().Get("from"), "from")
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	to, err := parseRevision(r.URL.Query().Get("to"), "to")
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	name := chi.URLParam(r, "name")
	files, err := s.skills.Diff(r.Context(), name, from, to)
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, skillDiffDTO(name, from, to, files))
}

func (s *Server) handleRollbackSkill(w http.ResponseWriter, r *http.Request) {
	var req RollbackSkillRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if req.Revision < 1 || req.BaseRevision < 1 {
		s.respondBadRequest(w, r, "revision and base_revision must be positive integers")
		return
	}
	overview, err := s.skills.Rollback(r.Context(), chi.URLParam(r, "name"), req.Revision, req.BaseRevision)
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, skillSummaryDTO(overview))
}

func (s *Server) handleRetireSkill(w http.ResponseWriter, r *http.Request) {
	s.handleSkillChange(w, r, s.skills.Retire)
}

func (s *Server) handleRestoreSkill(w http.ResponseWriter, r *http.Request) {
	s.handleSkillChange(w, r, s.skills.Restore)
}

// handleSkillChange writes the owner's change to a skill on top of the
// revision the owner read.
func (s *Server) handleSkillChange(w http.ResponseWriter, r *http.Request, change func(ctx context.Context, name string, baseRevision int64) (SkillOverview, error)) {
	var req SkillChangeRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if req.BaseRevision < 1 {
		s.respondBadRequest(w, r, "base_revision must be a positive integer")
		return
	}
	overview, err := change(r.Context(), chi.URLParam(r, "name"), req.BaseRevision)
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, skillSummaryDTO(overview))
}

func (s *Server) handleSetSkillSecret(w http.ResponseWriter, r *http.Request) {
	var req SetSkillSecretRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	secrets, err := s.skills.SetSecret(r.Context(), chi.URLParam(r, "name"), chi.URLParam(r, "secret"), req.Value)
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillSecretListResponse{Secrets: skillSecretDTOs(secrets)})
}

func (s *Server) handleClearSkillSecret(w http.ResponseWriter, r *http.Request) {
	secrets, err := s.skills.ClearSecret(r.Context(), chi.URLParam(r, "name"), chi.URLParam(r, "secret"))
	if err != nil {
		s.respondSkillError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillSecretListResponse{Secrets: skillSecretDTOs(secrets)})
}

func parseRevision(raw, field string) (int64, error) {
	revision, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || revision < 1 {
		return 0, errors.New(field + " must be a positive integer")
	}
	return revision, nil
}

func (s *Server) respondSkillError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, core.ErrSkillNotFound):
		s.respondNotFound(w, r, "skill_not_found", err.Error())
	case errors.Is(err, core.ErrSkillConflict):
		s.respondConflict(w, r, "skill_changed", "the skill changed since it was read; reload it and try again")
	case errors.Is(err, core.ErrSkillInvalid):
		s.respondError(w, r, http.StatusBadRequest, "skill_change_invalid", err.Error())
	default:
		s.respondKnownError(w, r, err)
	}
}
