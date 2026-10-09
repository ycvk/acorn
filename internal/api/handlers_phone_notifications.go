package api

import (
	"errors"
	"net/http"
)

func (s *Server) handlePhoneNotifications(w http.ResponseWriter, r *http.Request) {
	device, ok := DeviceFromContext(r.Context())
	if !ok {
		s.respondInternalError(w, r, errors.New("phone notifications require an authenticated device"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024*1024)
	var batch PhoneNotificationBatch
	if err := decodeJSONObject(r.Body, &batch); err != nil {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	accepted, err := s.phoneNotifications.Add(r.Context(), device.Device.DeviceID, batch)
	if errors.Is(err, ErrInvalidPhoneNotification) {
		s.respondBadRequest(w, r, err.Error())
		return
	}
	if err != nil {
		s.respondInternalError(w, r, err)
		return
	}
	s.respondJSON(w, r, http.StatusOK, accepted)
}
