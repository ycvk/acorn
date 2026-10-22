package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ycvk/acorn/internal/core"
	"github.com/ycvk/acorn/internal/settings"
)

// SettingsDTO is GET and PATCH /v1/settings. Customized names the settings
// the owner saved; the rest are at their defaults.
type SettingsDTO struct {
	Values     settings.Values `json:"values"`
	Defaults   settings.Values `json:"defaults"`
	Customized []string        `json:"customized"`
	Usage      settings.Usage  `json:"usage"`
}

// UpdateSettingsRequest is PATCH /v1/settings: settings to set by name, and
// names to return to their defaults.
type UpdateSettingsRequest struct {
	Set   map[string]json.RawMessage `json:"set"`
	Reset []string                   `json:"reset"`
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	current, err := s.settings.Current(r.Context())
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, settingsDTO(current))
}

func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		s.respondInternalError(w, r, errors.New("settings request reached the handler without an authenticated device"))
		return
	}
	var req UpdateSettingsRequest
	if err := decodeJSONBody(r, &req); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	change := settings.Change{Set: make(map[string]json.RawMessage, len(req.Set))}
	var errs []error
	for name, value := range req.Set {
		key, err := settings.KeyForName(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		change.Set[key] = value
	}
	for _, name := range req.Reset {
		key, err := settings.KeyForName(name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		change.Reset = append(change.Reset, key)
	}
	if err := errors.Join(errs...); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	current, err := s.settings.Save(r.Context(), change, device.Device.DeviceID)
	if errors.Is(err, core.ErrSettingInvalid) {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if err != nil {
		s.respondKnownError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, settingsDTO(current))
}

func settingsDTO(current settings.Current) SettingsDTO {
	customized := make([]string, len(current.Customized))
	for i, key := range current.Customized {
		customized[i] = settings.NameForKey(key)
	}
	return SettingsDTO{Values: current.Values, Defaults: current.Defaults, Customized: customized, Usage: current.Usage}
}
