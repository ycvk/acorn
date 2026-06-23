package api

import (
	"net/http"
	"strings"
)

func (s *Server) decodeCreateThreadRequest(w http.ResponseWriter, r *http.Request) (CreateThreadRequest, bool) {
	if r == nil || r.Body == nil || r.ContentLength == 0 {
		return CreateThreadRequest{}, true
	}
	var req CreateThreadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return CreateThreadRequest{}, false
	}
	req.Title = strings.TrimSpace(req.Title)
	return req, true
}

func (s *Server) decodeUpdateThreadRequest(w http.ResponseWriter, r *http.Request) (UpdateThreadRequest, bool) {
	var req UpdateThreadRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return UpdateThreadRequest{}, false
	}
	req.Title = strings.TrimSpace(req.Title)
	return req, true
}

func (s *Server) decodeCreateMessageRequest(w http.ResponseWriter, r *http.Request) (CreateMessageRequest, bool) {
	var req CreateMessageRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return CreateMessageRequest{}, false
	}
	req.Content.Type = strings.TrimSpace(req.Content.Type)
	req.Content.Text = strings.TrimSpace(req.Content.Text)
	if req.Content.Type != "text" {
		s.respondBadRequest(w, r, "content.type must be text")
		return CreateMessageRequest{}, false
	}
	if req.Content.Text == "" {
		s.respondBadRequest(w, r, "content.text is required")
		return CreateMessageRequest{}, false
	}
	return req, true
}

func (s *Server) decodeCreateRunRequest(w http.ResponseWriter, r *http.Request) (CreateRunRequest, bool) {
	if r == nil || r.Body == nil || r.ContentLength == 0 {
		return CreateRunRequest{}, true
	}
	var req CreateRunRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return CreateRunRequest{}, false
	}
	req.SkillID = strings.TrimSpace(req.SkillID)
	req.Input = strings.TrimSpace(req.Input)
	return req, true
}

func (s *Server) decodeDecidePendingActionRequest(w http.ResponseWriter, r *http.Request) (DecidePendingActionRequest, bool) {
	if r == nil || r.Body == nil || r.ContentLength == 0 {
		s.respondBadRequest(w, r, "request body is required")
		return DecidePendingActionRequest{}, false
	}
	var req DecidePendingActionRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return DecidePendingActionRequest{}, false
	}
	req.Decision = strings.TrimSpace(req.Decision)
	req.SelectedOptionID = strings.TrimSpace(req.SelectedOptionID)
	req.Answer = strings.TrimSpace(req.Answer)
	if req.Decision == "" {
		s.respondBadRequest(w, r, "decision is required")
		return DecidePendingActionRequest{}, false
	}
	return req, true
}
