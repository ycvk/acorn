package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/store"
)

func TestPhoneNotificationValidationAndOwnership(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	s, err := NewPhoneNotificationService(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	valid := PhoneNotificationInput{Key: "key", Package: "bank", App: "Bank", Title: "transfer", Text: "payment", PostedAt: now}
	for _, tc := range []struct {
		name   string
		change func(*PhoneNotificationInput)
	}{
		{"key", func(v *PhoneNotificationInput) { v.Key = "" }},
		{"key bytes", func(v *PhoneNotificationInput) { v.Key = strings.Repeat("界", 86) }},
		{"package", func(v *PhoneNotificationInput) { v.Package = " " }},
		{"app", func(v *PhoneNotificationInput) { v.App = "" }},
		{"title", func(v *PhoneNotificationInput) { v.Title = strings.Repeat("界", 201) }},
		{"text", func(v *PhoneNotificationInput) { v.Text = strings.Repeat("界", 2001) }},
		{"future", func(v *PhoneNotificationInput) { v.PostedAt = now.Add(6 * time.Minute) }},
		{"time", func(v *PhoneNotificationInput) { v.PostedAt = time.Time{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			v := valid
			tc.change(&v)
			if _, err := s.Add(context.Background(), "device", PhoneNotificationBatch{Notifications: []PhoneNotificationInput{valid, v}}); !errors.Is(err, ErrInvalidPhoneNotification) {
				t.Fatalf("error=%v", err)
			}
		})
	}
	page, err := db.ListPhoneNotifications(context.Background(), now.Add(-time.Hour), now.Add(time.Hour), 100)
	if err != nil || page.Total != 0 {
		t.Fatalf("invalid batch partly stored: %+v %v", page, err)
	}
	for i, want := range []int{1, 0} {
		out, err := s.Add(context.Background(), "device", PhoneNotificationBatch{Notifications: []PhoneNotificationInput{valid}})
		if err != nil || out.Accepted != want {
			t.Fatalf("attempt %d: %+v %v", i, out, err)
		}
	}
	page, err = db.ListPhoneNotifications(context.Background(), now.Add(-time.Hour), now.Add(time.Hour), 100)
	if err != nil || page.Items[0].DeviceID != "device" || !page.Items[0].ReceivedAt.Equal(now) {
		t.Fatalf("stored=%+v %v", page, err)
	}
}

func TestModelUsageIsDiagnostic(t *testing.T) {
	if IsLiveRunEventKind("model.usage") {
		t.Fatal("usage must not change the mobile live contract")
	}
}

func TestPhoneNotificationHandlerAcceptsMaximumUnicodeBatch(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	now := time.Now().UTC()
	service, err := NewPhoneNotificationService(db, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	batch := PhoneNotificationBatch{Notifications: make([]PhoneNotificationInput, 100)}
	for i := range batch.Notifications {
		batch.Notifications[i] = PhoneNotificationInput{Key: strings.Repeat("k", 256), Package: strings.Repeat("😀", 256), App: strings.Repeat("😀", 256), Title: strings.Repeat("😀", 200), Text: strings.Repeat("😀", 2000), PostedAt: now.Add(-time.Duration(i) * time.Second)}
	}
	body, err := json.Marshal(batch)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{phoneNotifications: service, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	req := httptest.NewRequest(http.MethodPost, "/v1/phone-notifications", bytes.NewReader(body))
	req = req.WithContext(withDevice(req.Context(), &DeviceAuthContext{Device: DeviceView{DeviceID: "phone"}}))
	response := httptest.NewRecorder()
	server.handlePhoneNotifications(response, req)
	if response.Code != http.StatusOK {
		t.Fatalf("batch of %d bytes rejected: %s", len(body), response.Body.String())
	}
	var result PhoneNotificationAccepted
	if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil || result.Accepted != 100 {
		t.Fatalf("accepted=%+v %v", result, err)
	}
}
