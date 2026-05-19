package web

import (
	"net/http"
)

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	s.respondJSON(w, r, http.StatusOK, HealthResponse{OK: true})
}
