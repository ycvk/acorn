package presence

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func TestPhoneNotificationsDropBeforeMemoryWithoutMutatingInput(t *testing.T) {
	phones := []core.PhoneNotification{{App: "Bank", Title: "new", Text: strings.Repeat("界", 150), PostedAt: t0, ReceivedAt: t0}, {App: "Mail", Title: "old", Text: "hello", PostedAt: t0.Add(-time.Hour), ReceivedAt: t0.Add(-time.Hour)}}
	before := append([]core.PhoneNotification(nil), phones...)
	input := RenderInput{Now: t0, Location: time.UTC, Wake: "owner message", PhoneNotifications: phones, Thoughts: []core.MemoryRecord{{ID: "thought", Kind: "thought", State: "open", Content: "keep this thought", UpdatedAt: t0}}, Count: runeCount}
	full := mustRender(t, input)
	if !strings.Contains(full, "## Phone notifications") || strings.Contains(full, strings.Repeat("界", 121)) {
		t.Fatalf("render=%s", full)
	}
	input.MaxTokens = 260
	short := mustRender(t, input)
	if strings.Contains(short, "Phone notifications") || !strings.Contains(short, "keep this thought") {
		t.Fatalf("drop priority=%s", short)
	}
	if !reflect.DeepEqual(phones, before) {
		t.Fatal("render mutated caller notifications")
	}
}
