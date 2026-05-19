package web

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/app"
)

func (s *Server) handleListSkills(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	offset, err := parseOffset(r.URL.Query().Get("offset"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, total, err := s.skills.ListFiltered(r.Context(), app.SkillListFilter{
		Limit:  limit,
		Offset: offset,
	})
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillListResponse{
		Items: skillSummaryDTOsFromViews(items),
		Total: total,
	})
}

func (s *Server) handleGetSkill(w http.ResponseWriter, r *http.Request) {
	item, err := s.skills.Get(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillEnvelope{Item: skillDetailDTOFromView(*item)})
}

func (s *Server) handleReadSkillFile(w http.ResponseWriter, r *http.Request) {
	item, err := s.skills.ReadFile(r.Context(), chi.URLParam(r, "id"), r.URL.Query().Get("path"))
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, SkillFileResponse{Item: *item})
}
