package notify

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
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
	if err := c.Send(context.Background(), "fid_1", Message{Title: "提醒", Body: "该看 X 了", ThreadID: "thread_1", RunID: "run_1"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg := got["message"]
	if msg["fid"] != "fid_1" || msg["token"] != nil {
		t.Fatalf("target = fid %v token %v, want fid fid_1", msg["fid"], msg["token"])
	}
	if n := msg["notification"].(map[string]any); n["title"] != "提醒" || n["body"] != "该看 X 了" {
		t.Fatalf("notification = %v", n)
	}
	if d := msg["data"].(map[string]any); d["thread_id"] != "thread_1" || d["run_id"] != "run_1" || d["browser_url"] != nil {
		t.Fatalf("data = %v", d)
	}
	if err := c.Send(context.Background(), "fid_1", Message{Title: "需要登录", Body: "在浏览器里登录 GitHub", ThreadID: "thread_1", RunID: "run_1", BrowserURL: "https://github.com/login"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if d := got["message"]["data"].(map[string]any); d["thread_id"] != "thread_1" || d["browser_url"] != "https://github.com/login" {
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
		{"unregistered", http.StatusNotFound, `{"error":{"status":"NOT_FOUND"}}`, ErrUnregistered, 1},
		{"unregistered detail", http.StatusBadRequest, `{"error":{"details":[{"errorCode":"UNREGISTERED"}]}}`, ErrUnregistered, 1},
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
	mu    sync.Mutex
	fids  map[string]string
	notes []core.Notification
}

func (m *memNotifications) SetPushInstallation(_ context.Context, deviceID, fid string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fids[deviceID] = fid
	return nil
}

func (m *memNotifications) ListPushInstallations(context.Context) ([]core.PushInstallation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []core.PushInstallation
	for _, id := range []string{"dev_a", "dev_b"} {
		if fid, ok := m.fids[id]; ok {
			out = append(out, core.PushInstallation{DeviceID: id, FID: fid})
		}
	}
	return out, nil
}

func (m *memNotifications) DeletePushInstallation(_ context.Context, deviceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fids, deviceID)
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

func (p *recordingPusher) Send(_ context.Context, fid string, msg Message) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err := p.errs[fid]; err != nil {
		return err
	}
	p.sent = append(p.sent, fid+":"+msg.Title)
	return nil
}

type senderHarness struct {
	store    *memNotifications
	pusher   *recordingPusher
	sender   *Sender
	now      time.Time
	settings core.Settings
	actions  map[string]core.PendingActionStatus
}

func (h *senderHarness) Settings(context.Context) (core.Settings, error) { return h.settings, nil }

func (h *senderHarness) LoadPendingAction(_ context.Context, actionID string) (*core.PendingActionRecord, error) {
	status, ok := h.actions[actionID]
	if !ok {
		return nil, fmt.Errorf("%w: %s", core.ErrPendingActionNotFound, actionID)
	}
	return &core.PendingActionRecord{ActionID: actionID, Status: status}, nil
}

func newSenderHarness(t *testing.T, quiet core.QuietHours) *senderHarness {
	t.Helper()
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		t.Fatal(err)
	}
	h := &senderHarness{
		store:    &memNotifications{fids: map[string]string{"dev_a": "fid_a"}},
		pusher:   &recordingPusher{errs: map[string]error{}},
		now:      time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC), // 12:00 Shanghai
		settings: core.Settings{Location: loc, MaxPushesPerHour: 2, QuietHours: quiet},
		actions:  map[string]core.PendingActionStatus{},
	}
	h.sender, err = NewSender(SenderConfig{
		Store: h.store, Actions: h, Pusher: h.pusher, Clock: func() time.Time { return h.now }, Settings: h,
	})
	if err != nil {
		t.Fatal(err)
	}
	return h
}

var overnight = core.QuietHours{Start: 23 * time.Hour, End: 8 * time.Hour}

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
	h.settings.MaxPushesPerHour = 3
	if _, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("notify after the owner raised the cap: %v", err)
	}
	h.now = h.now.Add(61 * time.Minute)
	if _, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"}); err != nil {
		t.Fatalf("notify after an hour: %v", err)
	}
}

func TestNotifyReadsTheQuietHoursTheOwnerSavedLast(t *testing.T) {
	h := newSenderHarness(t, overnight)
	ctx := context.Background()
	h.settings.QuietHours = core.QuietHours{Start: 11 * time.Hour, End: 13 * time.Hour}
	n, err := h.sender.Notify(ctx, core.Notification{Title: "午间", Body: "b"})
	if err != nil || n.Status != core.NotificationQueued {
		t.Fatalf("notify at 12:00 inside the new window = %+v err=%v", n, err)
	}
	if want := time.Date(2026, 10, 5, 5, 0, 0, 0, time.UTC); !n.SendAfter.Equal(want) {
		t.Fatalf("send after = %v, want 13:00 Shanghai %v", n.SendAfter, want)
	}
	h.settings.Location = time.UTC
	n, err = h.sender.Notify(ctx, core.Notification{Title: "UTC", Body: "b"})
	if err != nil || n.Status != core.NotificationSent {
		t.Fatalf("notify at 04:00 UTC outside the window = %+v err=%v", n, err)
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

// What the owner is waiting for goes out inside quiet hours. The hourly cap
// applies to it like to any push.
func TestNotifyAwaitedIsSentInsideQuietHours(t *testing.T) {
	h := newSenderHarness(t, overnight)
	ctx := context.Background()
	h.now = time.Date(2026, 10, 5, 16, 56, 0, 0, time.UTC) // 00:56 Shanghai
	h.actions["a1"] = core.PendingActionStatusPending
	n, err := h.sender.NotifyAwaited(ctx, core.Notification{Title: "Approve skill_write", Body: "b", ThreadID: "thread_1", ActionID: "a1"})
	if err != nil || n.Status != core.NotificationSent || !n.SendAfter.Equal(h.now) {
		t.Fatalf("awaited inside quiet hours = %+v err=%v", n, err)
	}
	if len(h.pusher.sent) != 1 {
		t.Fatalf("pushed = %v", h.pusher.sent)
	}
	if n, err := h.sender.Notify(ctx, core.Notification{Title: "夜里", Body: "b"}); err != nil || n.Status != core.NotificationQueued {
		t.Fatalf("another push inside quiet hours = %+v err=%v", n, err)
	}
	if _, err := h.sender.NotifyAwaited(ctx, core.Notification{Title: "Approve skill_write", Body: "b", ActionID: "a1"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("awaited over the hourly cap = %v, want rate limited", err)
	}
}

func TestFlushDropsQueuedPushesForDecidedActions(t *testing.T) {
	h := newSenderHarness(t, overnight)
	h.settings.MaxPushesPerHour = 5
	ctx := context.Background()
	h.now = time.Date(2026, 10, 5, 16, 30, 0, 0, time.UTC) // 00:30 Shanghai
	store := announceStore{
		run: core.RunRecord{RunID: "run_1", SessionID: "thread_1"},
		actions: []core.PendingActionRecord{
			{ActionID: "a1", Kind: core.PendingActionKindToolApproval, Subject: "发送邮件", PayloadJSON: `{"message":"给张三"}`, Status: core.PendingActionStatusPending},
			{ActionID: "a2", Kind: core.PendingActionKindOperatorQuestion, PayloadJSON: `{"question":"周几方便？"}`, Status: core.PendingActionStatusPending},
		},
	}
	h.actions["a1"], h.actions["a2"] = core.PendingActionStatusPending, core.PendingActionStatusPending
	announcer, err := NewPendingActionAnnouncer(store, h.sender)
	if err != nil {
		t.Fatal(err)
	}
	if err := announcer.AnnounceRun(ctx, "run_1"); err != nil {
		t.Fatalf("announce: %v", err)
	}
	if _, err := h.sender.Notify(ctx, core.Notification{Title: "夜里", Body: "b"}); err != nil {
		t.Fatalf("notify: %v", err)
	}
	h.actions["a1"] = core.PendingActionStatusRejected

	h.now = time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC) // 08:00 Shanghai
	if err := h.sender.FlushDue(ctx); err != nil {
		t.Fatalf("flush: %v", err)
	}
	if want := []string{"fid_a:operator_question", "fid_a:夜里"}; strings.Join(h.pusher.sent, ",") != strings.Join(want, ",") {
		t.Fatalf("sent = %v, want %v", h.pusher.sent, want)
	}
	if n := h.store.notes[0]; n.Status != core.NotificationDropped || !strings.Contains(n.Error, "rejected") {
		t.Fatalf("push for the decided action = %+v, want dropped", n)
	}
	for _, n := range h.store.notes[1:] {
		if n.Status != core.NotificationSent {
			t.Fatalf("push %d = %+v, want sent", n.ID, n)
		}
	}
}

func TestQuietHoursWindows(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 10, 5, h, m, 0, 0, loc) }
	for _, tc := range []struct {
		q       core.QuietHours
		t       time.Time
		quiet   bool
		endHour int
	}{
		{overnight, at(23, 0), true, 8},
		{overnight, at(7, 59), true, 8},
		{overnight, at(8, 0), false, 0},
		{core.QuietHours{Start: 13 * time.Hour, End: 14 * time.Hour}, at(13, 30), true, 14},
		{core.QuietHours{Start: 13 * time.Hour, End: 14 * time.Hour}, at(14, 0), false, 0},
		{core.QuietHours{}, at(3, 0), false, 0},
	} {
		end, quiet := quietEnd(tc.q, tc.t)
		if quiet != tc.quiet || (quiet && end.Hour() != tc.endHour) {
			t.Fatalf("%v at %v: quiet=%v end=%v", tc.q, tc.t, quiet, end)
		}
	}
}

func TestNotifyFailsWithoutDevices(t *testing.T) {
	h := newSenderHarness(t, core.QuietHours{})
	h.store.fids = map[string]string{}
	if _, err := h.sender.Notify(context.Background(), core.Notification{Title: "t", Body: "b"}); !errors.Is(err, ErrNoPushInstallation) {
		t.Fatalf("err = %v", err)
	}
}

func TestDeliverDropsUnregisteredInstallations(t *testing.T) {
	h := newSenderHarness(t, core.QuietHours{})
	ctx := context.Background()
	h.store.fids["dev_b"] = "fid_b"
	h.pusher.errs["fid_a"] = ErrUnregistered
	n, err := h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"})
	if err != nil || n.Status != core.NotificationSent || len(h.pusher.sent) != 1 {
		t.Fatalf("notify = %+v err=%v sent=%v", n, err, h.pusher.sent)
	}
	if _, ok := h.store.fids["dev_a"]; ok {
		t.Fatal("unregistered installation must be deleted")
	}
	h.pusher.errs["fid_b"] = errors.New("fcm down")
	_, err = h.sender.Notify(ctx, core.Notification{Title: "t", Body: "b"})
	if err == nil || !strings.Contains(err.Error(), "fcm down") {
		t.Fatalf("all-failed notify = %v", err)
	}
	if last := h.store.notes[len(h.store.notes)-1]; last.Status != core.NotificationFailed || !strings.Contains(last.Error, "fcm down") {
		t.Fatalf("failed notification = %+v", last)
	}
}

// A pending action push carries what the app needs to load the action and
// none of the action's own text, so it fits in an FCM message whatever the
// action asks.
func TestFCMSendsPendingActionsAsData(t *testing.T) {
	var got map[string]map[string]any
	c := testFCM(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) > 4096 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"code":400,"message":"Message is too large. The maximum is 4K (4096 bytes).","status":"INVALID_ARGUMENT"}}`)
			return
		}
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})
	wholeSkill := "skill_write wants to run with arguments:\n" + strings.Repeat("下载视频后删掉一天前的文件。", 400)
	if err := c.Send(context.Background(), "fid_1", Message{Title: "Approve skill_write", Body: wholeSkill, ThreadID: "thread_1", RunID: "run_1", ActionID: "action_1"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	msg := got["message"]
	if _, ok := msg["notification"]; ok {
		t.Fatalf("pending action push has a notification block: %v", msg)
	}
	d := msg["data"].(map[string]any)
	if d["type"] != "pending_action" || d["action_id"] != "action_1" || d["thread_id"] != "thread_1" || d["run_id"] != "run_1" || d["title"] != "Approve skill_write" {
		t.Fatalf("data = %v", d)
	}
	if _, ok := d["body"]; ok {
		t.Fatalf("pending action push carries the action's text: %d bytes", len(d["body"].(string)))
	}
}

// A push FCM would refuse is refused when it is asked for, inside quiet
// hours too, so whoever asked learns of it then; nothing is queued. An
// announcement of a pending action is sent whatever its stored text.
func TestNotifyRefusesAPushOverTheFCMLimit(t *testing.T) {
	h := newSenderHarness(t, overnight)
	ctx := context.Background()
	long := strings.Repeat("长", 1400)
	for _, hour := range []int{4, 16} { // 12:00 and 00:00 Shanghai
		h.now = time.Date(2026, 10, 5, hour, 0, 0, 0, time.UTC)
		_, err := h.sender.Notify(ctx, core.Notification{Title: "进展", Body: long, ThreadID: "thread_1"})
		if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), "4096") {
			t.Fatalf("notify at %d:00 UTC = %v, want too large with the limit", hour, err)
		}
	}
	if len(h.store.notes) != 0 || len(h.pusher.sent) != 0 {
		t.Fatalf("refused pushes left notes %+v, sent %v", h.store.notes, h.pusher.sent)
	}
	h.now = time.Date(2026, 10, 5, 4, 0, 0, 0, time.UTC)
	h.actions["action_1"] = core.PendingActionStatusPending
	n, err := h.sender.Notify(ctx, core.Notification{Title: "Approve skill_write", Body: long, ThreadID: "thread_1", ActionID: "action_1"})
	if err != nil || n.Status != core.NotificationSent {
		t.Fatalf("announce a large action: %+v err=%v", n, err)
	}
	if n, err := h.sender.Notify(ctx, core.Notification{Title: "进展", Body: strings.Repeat("长", 1200), ThreadID: "thread_1"}); err != nil || n.Status != core.NotificationSent {
		t.Fatalf("notify under the limit: %+v err=%v", n, err)
	}
}

type announceStore struct {
	run       core.RunRecord
	actions   []core.PendingActionRecord
	ownerLast time.Time
}

func (s announceStore) LoadRun(context.Context, string) (*core.RunRecord, error) { return &s.run, nil }
func (s announceStore) ListPendingActionsByRun(context.Context, string) ([]core.PendingActionRecord, error) {
	return s.actions, nil
}
func (s announceStore) LatestOwnerMessageAt(context.Context, string) (time.Time, error) {
	return s.ownerLast, nil
}

type recordingNotifier struct{ sent, awaited []core.Notification }

func (r *recordingNotifier) Notify(_ context.Context, n core.Notification) (core.Notification, error) {
	r.sent = append(r.sent, n)
	return n, nil
}

func (r *recordingNotifier) NotifyAwaited(_ context.Context, n core.Notification) (core.Notification, error) {
	r.awaited = append(r.awaited, n)
	return n, nil
}

// An action asked within half an hour of the owner's last message in its
// conversation is something the owner is waiting for. One asked later, or in
// a conversation the owner never wrote in, is announced like any push.
func TestAnnounceRunTellsWhatTheOwnerIsWaitingFor(t *testing.T) {
	asked := time.Date(2026, 11, 10, 16, 56, 0, 0, time.UTC)
	for _, tc := range []struct {
		name      string
		ownerLast time.Time
		awaited   bool
	}{
		{"five minutes after the owner wrote", asked.Add(-5 * time.Minute), true},
		{"half an hour after", asked.Add(-30 * time.Minute), true},
		{"later than that", asked.Add(-31 * time.Minute), false},
		{"the owner never wrote here", time.Time{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := announceStore{
				run: core.RunRecord{RunID: "run_1", SessionID: "thread_1"},
				actions: []core.PendingActionRecord{
					{ActionID: "a1", Kind: core.PendingActionKindToolApproval, Subject: "Approve skill_write", PayloadJSON: `{}`, Status: core.PendingActionStatusPending, CreatedAt: asked},
				},
				ownerLast: tc.ownerLast,
			}
			notifier := &recordingNotifier{}
			announcer, err := NewPendingActionAnnouncer(store, notifier)
			if err != nil {
				t.Fatal(err)
			}
			if err := announcer.AnnounceRun(context.Background(), "run_1"); err != nil {
				t.Fatalf("announce: %v", err)
			}
			got, other := notifier.sent, notifier.awaited
			if tc.awaited {
				got, other = other, got
			}
			if len(got) != 1 || len(other) != 0 || got[0].ActionID != "a1" || got[0].ThreadID != "thread_1" {
				t.Fatalf("awaited = %+v, held by quiet hours = %+v", notifier.awaited, notifier.sent)
			}
		})
	}
}

func TestAnnounceRunPushesOnlyPendingActions(t *testing.T) {
	store := announceStore{
		run: core.RunRecord{RunID: "run_1", SessionID: "thread_1"},
		actions: []core.PendingActionRecord{
			{ActionID: "a1", Kind: core.PendingActionKindToolApproval, Subject: "发送邮件", PayloadJSON: `{"message":"给张三"}`, Status: core.PendingActionStatusPending},
			{ActionID: "a2", Kind: core.PendingActionKindOperatorQuestion, PayloadJSON: `{"question":"周几方便？"}`, Status: core.PendingActionStatusPending},
			{ActionID: "a3", Kind: core.PendingActionKindToolApproval, Subject: "已经决定", PayloadJSON: `{}`, Status: core.PendingActionStatusApproved},
		},
	}
	notifier := &recordingNotifier{}
	announcer, err := NewPendingActionAnnouncer(store, notifier)
	if err != nil {
		t.Fatal(err)
	}
	if err := announcer.AnnounceRun(context.Background(), "run_1"); err != nil {
		t.Fatalf("announce: %v", err)
	}
	want := []core.Notification{
		{Title: "发送邮件", Body: "给张三", ThreadID: "thread_1", RunID: "run_1", ActionID: "a1"},
		{Title: "operator_question", Body: "周几方便？", ThreadID: "thread_1", RunID: "run_1", ActionID: "a2"},
	}
	if len(notifier.sent) != len(want) {
		t.Fatalf("sent = %+v", notifier.sent)
	}
	for i := range want {
		if notifier.sent[i] != want[i] {
			t.Fatalf("sent[%d] = %+v, want %+v", i, notifier.sent[i], want[i])
		}
	}
}
