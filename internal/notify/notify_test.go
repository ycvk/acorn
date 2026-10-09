package notify

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func testFCM(t *testing.T, handler http.HandlerFunc) *FCMClient {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := newFCMClient(server.Client(), server.URL)
	c.backoff = func(int) time.Duration { return time.Millisecond }
	return c
}

func TestFCMSendBuildsMessage(t *testing.T) {
	var got map[string]map[string]any
	c := testFCM(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := c.Send(context.Background(), "tok_1", Message{Title: "提醒", Body: "该看 X 了", ThreadID: "thread_1", RunID: "run_1"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg := got["message"]
	if msg["token"] != "tok_1" {
		t.Fatalf("token = %v", msg["token"])
	}
	if n := msg["notification"].(map[string]any); n["title"] != "提醒" || n["body"] != "该看 X 了" {
		t.Fatalf("notification = %v", n)
	}
	if d := msg["data"].(map[string]any); d["thread_id"] != "thread_1" || d["run_id"] != "run_1" {
		t.Fatalf("data = %v", d)
	}
}

func TestFCMClientAuthenticatesWithServiceAccount(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil || r.Form.Get("assertion") == "" {
			t.Errorf("token request without assertion: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"fcm-access","token_type":"Bearer","expires_in":3600}`)
	}))
	t.Cleanup(tokenServer.Close)
	var auth string
	fcmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(fcmServer.Close)

	client, err := NewFCMClientAt(ServiceAccount{ProjectID: "p", ClientEmail: "acorn@p.iam.gserviceaccount.com", PrivateKey: string(keyPEM), TokenURI: tokenServer.URL}, fcmServer.URL)
	if err != nil {
		t.Fatalf("client: %v", err)
	}
	if err := client.Send(context.Background(), "t", Message{Title: "a", Body: "b"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if auth != "Bearer fcm-access" {
		t.Fatalf("authorization = %q", auth)
	}
	if _, err := NewFCMClient(ServiceAccount{ProjectID: "p"}); err == nil {
		t.Fatal("incomplete service account must fail")
	}
}

func TestFCMSendRetriesThenSucceeds(t *testing.T) {
	var calls int
	c := testFCM(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	if err := c.Send(context.Background(), "t", Message{Title: "a", Body: "b"}); err != nil || calls != 3 {
		t.Fatalf("send: err=%v calls=%d", err, calls)
	}
}

func TestFCMSendSurfacesFailures(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		want   error
		calls  int
	}{
		{"unregistered", http.StatusNotFound, `{"error":{"status":"NOT_FOUND"}}`, ErrTokenUnregistered, 1},
		{"unregistered detail", http.StatusBadRequest, `{"error":{"details":[{"errorCode":"UNREGISTERED"}]}}`, ErrTokenUnregistered, 1},
		{"bad request", http.StatusBadRequest, `{"error":"bad"}`, nil, 1},
		{"server down", http.StatusInternalServerError, `oops`, nil, fcmMaxAttempts},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			c := testFCM(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			err := c.Send(context.Background(), "t", Message{Title: "a", Body: "b"})
			if err == nil || calls != tc.calls {
				t.Fatalf("err=%v calls=%d, want %d calls", err, calls, tc.calls)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// memNotifications is an in-memory core.NotificationStore.
type memNotifications struct {
	mu     sync.Mutex
	tokens map[string]string
	notes  []core.Notification
}

func (m *memNotifications) SetPushToken(_ context.Context, deviceID, token string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tokens[deviceID] = token
	return nil
}

func (m *memNotifications) ListPushTokens(context.Context) ([]core.PushToken, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.PushToken
	for _, id := range []string{"dev_a", "dev_b"} {
		if token, ok := m.tokens[id]; ok {
			out = append(out, core.PushToken{DeviceID: id, Token: token})
		}
	}
	return out, nil
}

func (m *memNotifications) DeletePushToken(_ context.Context, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, deviceID)
	return nil
}

func (m *memNotifications) QueueNotification(_ context.Context, n core.Notification) (core.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n.ID, n.Status, n.CreatedAt = int64(len(m.notes)+1), core.NotificationQueued, n.SendAfter
	m.notes = append(m.notes, n)
	return n, nil
}

func (m *memNotifications) ListDueNotifications(_ context.Context, now time.Time) ([]core.Notification, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.Notification
	for _, n := range m.notes {
		if n.Status == core.NotificationQueued && !n.SendAfter.After(now) {
			out = append(out, n)
		}
	}
	return out, nil
}

func (m *memNotifications) FinishNotification(_ context.Context, id int64, status core.NotificationStatus, errText string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.notes[id-1].Status, m.notes[id-1].Error, m.notes[id-1].SentAt = status, errText, at
	return nil
}

func (m *memNotifications) CountNotificationsSince(_ context.Context, since time.Time) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, note := range m.notes {
		if !note.CreatedAt.Before(since) {
			n++
		}
	}
	return n, nil
}

type recordingPusher struct {
	mu   sync.Mutex
	sent []string
	errs map[string]error
}

func (p *recordingPusher) Send(_ context.Context, token string, msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.errs[token]; err != nil {
		return err
	}
	p.sent = append(p.sent, token+":"+msg.Title)
	return nil
}

type senderHarness struct {
	store  *memNotifications
	pusher *recordingPusher
	sender *Sender
	now    time.Time
}

func newSenderHarness(t *testing.T, quiet QuietHours) *senderHarness {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	h := &senderHarness{
		store:  &memNotifications{tokens: map[string]string{"dev_a": "tok_a"}},
		pusher: &recordingPusher{errs: map[string]error{}},
		now:    time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), // 12:00 Shanghai
	}
	h.sender, err = NewSender(SenderConfig{
		Store: h.store, Pusher: h.pusher, Clock: func() time.Time { return h.now },
		Location: loc, MaxPerHour: 2, Quiet: quiet,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

var overnight = QuietHours{Start: 23 * time.Hour, End: 8 * time.Hour}

func TestNotifySendsNowAndEnforcesHourlyCap(t *testing.T) {
	h := newSenderHarness(t, overnight)
	ctx := context.Background()
	for i := range 2 {
		n, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b", ThreadID: "thread_1"})
		if err != nil || n.Status != core.NotificationSent {
			t.Fatalf("notify %d: %+v err=%v", i, n, err)
		}
	}
	if _, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("third notify = %v, want rate limited", err)
	}
	h.now = h.now.Add(61 * time.Minute)
	if _, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("notify after an hour: %v", err)
	}
}

func TestNotifyQueuesInsideQuietHoursAndFlushesAfter(t *testing.T) {
	h := newSenderHarness(t, overnight)
	ctx := context.Background()
	h.now = time.Date(2026, 10, 5, 16, 30, 0, 0, time.UTC) // 00:30 Shanghai
	n, err := h.sender.Notify(ctx, core.Notification{Title: "夜里", Body: "b"})
	if err != nil || n.Status != core.NotificationQueued {
		t.Fatalf("notify = %+v err=%v", n, err)
	}
	if want := time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC); !n.SendAfter.Equal(want) {
		t.Fatalf("send after = %v, want 08:00 Shanghai %v", n.SendAfter, want)
	}
	if err := h.sender.FlushDue(ctx); err != nil || len(h.pusher.sent) != 0 {
		t.Fatalf("flushed during quiet hours: sent=%v err=%v", h.pusher.sent, err)
	}
	h.now = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)
	if err := h.sender.FlushDue(ctx); err != nil || len(h.pusher.sent) != 1 {
		t.Fatalf("flush after quiet hours: sent=%v err=%v", h.pusher.sent, err)
	}
	if h.store.notes[0].Status != core.NotificationSent {
		t.Fatalf("status = %s", h.store.notes[0].Status)
	}
}

func TestQuietHoursWindows(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 10, 5, h, m, 0, 0, loc) }
	for _, tc := range []struct {
		q       QuietHours
		t       time.Time
		quiet   bool
		endHour int
	}{
		{overnight, at(23, 0), true, 8},
		{overnight, at(7, 59), true, 8},
		{overnight, at(8, 0), false, 0},
		{QuietHours{Start: 13 * time.Hour, End: 14 * time.Hour}, at(13, 30), true, 14},
		{QuietHours{Start: 13 * time.Hour, End: 14 * time.Hour}, at(14, 0), false, 0},
		{QuietHours{}, at(3, 0), false, 0},
	} {
		end, quiet := tc.q.endAfter(tc.t)
		if quiet != tc.quiet || (quiet && end.Hour() != tc.endHour) {
			t.Fatalf("%v at %v: quiet=%v end=%v", tc.q, tc.t, quiet, end)
		}
	}
}

func TestNotifyFailsWithoutDevices(t *testing.T) {
	h := newSenderHarness(t, QuietHours{})
	h.store.tokens = map[string]string{}
	if _, err := h.sender.Notify(context.Background(), core.Notification{Title: "t", Body: "b"}); !errors.Is(err, ErrNoPushToken) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeliverDropsUnregisteredTokens(t *testing.T) {
	h := newSenderHarness(t, QuietHours{})
	ctx := context.Background()
	h.store.tokens["dev_b"] = "tok_b"
	h.pusher.errs["tok_a"] = ErrTokenUnregistered
	n, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"})
	if err != nil || n.Status != core.NotificationSent || len(h.pusher.sent) != 1 {
		t.Fatalf("notify = %+v err=%v sent=%v", n, err, h.pusher.sent)
	}
	if _, ok := h.store.tokens["dev_a"]; ok {
		t.Fatal("unregistered token must be deleted")
	}
	h.pusher.errs["tok_b"] = errors.New("fcm down")
	_, err = h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "fcm down") {
		t.Fatalf("all-failed notify = %v", err)
	}
	if last := h.store.notes[len(h.store.notes)-1]; last.Status != core.NotificationFailed || !strings.Contains(last.Error, "fcm down") {
		t.Fatalf("failed notification = %+v", last)
	}
}
