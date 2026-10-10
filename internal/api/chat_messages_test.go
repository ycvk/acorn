package api

import (
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

func TestProjectCaptureMessage(t *testing.T) {
	if _, err := projectMessage(core.SessionMessageRecord{ID: 4, Role: core.MessageRoleCapture, Content: "x"}); err != nil {
		t.Fatal(err)
	}
}
