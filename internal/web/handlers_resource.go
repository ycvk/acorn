package web

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/ycvk/acorn/internal/app"
	"github.com/ycvk/acorn/internal/memorymodule"
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

func (s *Server) handleListMemoryFacts(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	selection, err := parseRecordSelection(r)
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.memory.ListFacts(r.Context(), selection)
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, MemoryRecordListResponse{Items: memoryRecordDTOsFromDomain(limitRecords(items, limit))})
}

func (s *Server) handleListMemorySkills(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	selection, err := parseRecordSelection(r)
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.memory.ListSkills(r.Context(), selection)
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, MemoryRecordListResponse{Items: memoryRecordDTOsFromDomain(limitRecords(items, limit))})
}

func (s *Server) handleListMemoryHistory(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	selection, err := parseRecordSelection(r)
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	items, err := s.memory.ListHistory(r.Context(), selection)
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, MemoryRecordListResponse{Items: memoryRecordDTOsFromDomain(limitRecords(items, limit))})
}

func (s *Server) handleSearchMemory(w http.ResponseWriter, r *http.Request) {
	limit, err := parseLimit(r.URL.Query().Get("limit"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	kinds, err := parseMemoryKinds(r.URL.Query().Get("kind"))
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	selection, err := parseRecordSelection(r)
	if err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	result, err := s.memory.Search(r.Context(), memorymodule.SearchRequest{
		Query:           strings.TrimSpace(r.URL.Query().Get("query")),
		Scope:           strings.TrimSpace(r.URL.Query().Get("scope")),
		Kinds:           kinds,
		Limit:           limit,
		IncludeInactive: selection.IncludeInactive,
		IncludeRetired:  selection.IncludeRetired,
	})
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	items := []memorymodule.SearchItem{}
	if result != nil {
		items = result.Items
	}
	s.respondJSON(w, r, http.StatusOK, MemorySearchResponse{Items: memorySearchItemDTOsFromDomain(items)})
}

func parseMemoryKinds(raw string) ([]memorymodule.Kind, error) {
	parts := strings.Split(raw, ",")
	kinds := make([]memorymodule.Kind, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		switch kind := memorymodule.Kind(trimmed); kind {
		case "":
		case memorymodule.KindFact, memorymodule.KindSkill, memorymodule.KindHistory:
			kinds = append(kinds, kind)
		default:
			return nil, fmt.Errorf("invalid memory kind %q", trimmed)
		}
	}
	return kinds, nil
}

func parseRecordSelection(r *http.Request) (memorymodule.RecordSelection, error) {
	includeInactive, err := parseBoolQuery(r, "include_inactive")
	if err != nil {
		return memorymodule.RecordSelection{}, err
	}
	includeRetired, err := parseBoolQuery(r, "include_retired")
	if err != nil {
		return memorymodule.RecordSelection{}, err
	}
	return memorymodule.RecordSelection{
		IncludeInactive: includeInactive,
		IncludeRetired:  includeRetired,
	}, nil
}

func parseBoolQuery(r *http.Request, key string) (bool, error) {
	raw := strings.TrimSpace(r.URL.Query().Get(key))
	switch raw {
	case "":
		return false, nil
	case "true":
		return true, nil
	case "false":
		return false, nil
	default:
		return false, fmt.Errorf("%s must be true or false", key)
	}
}

func limitRecords(items []memorymodule.Record, limit int) []memorymodule.Record {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func (s *Server) handleGetWorkingCheckpoint(w http.ResponseWriter, r *http.Request) {
	item, err := s.checkpoints.Get(r.Context(), chi.URLParam(r, "thread_id"))
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, WorkingCheckpointEnvelope{Item: workingCheckpointDTOFromView(item)})
}

func (s *Server) handleUpdateWorkingCheckpoint(w http.ResponseWriter, r *http.Request) {
	var req UpdateWorkingCheckpointRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	item, err := s.checkpoints.Update(r.Context(), chi.URLParam(r, "thread_id"), req.Content, req.RelatedSkillID)
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, WorkingCheckpointEnvelope{Item: workingCheckpointDTOFromView(item)})
}

func (s *Server) handleDeleteWorkingCheckpoint(w http.ResponseWriter, r *http.Request) {
	if err := s.checkpoints.Clear(r.Context(), chi.URLParam(r, "thread_id")); err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, r, http.StatusOK, HealthResponse{OK: true})
}
