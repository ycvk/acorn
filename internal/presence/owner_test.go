package presence

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

type ownerNameStore struct{ saved []string }

func (s *ownerNameStore) SetOwnerName(_ context.Context, name string, at time.Time) (core.OwnerName, error) {
	s.saved = append(s.saved, name)
	return core.OwnerName{Name: name, UpdatedAt: at}, nil
}

func (s *ownerNameStore) OwnerName(context.Context) (core.OwnerName, error) {
	return core.OwnerName{}, nil
}

func TestSaveOwnerNameTrimsAndRejectsWhatCannotBeAName(t *testing.T) {
	ctx := t.Context()
	at := time.Date(2026, 10, 18, 9, 0, 0, 0, time.UTC)
	store := &ownerNameStore{}
	for in, want := range map[string]string{"  温遥 ": "温遥", "": "", "   ": "", strings.Repeat("遥", MaxOwnerNameRunes): strings.Repeat("遥", MaxOwnerNameRunes)} {
		owner, err := SaveOwnerName(ctx, store, in, at)
		if err != nil || owner.Name != want {
			t.Fatalf("save %q = %+v, %v; want %q", in, owner, err, want)
		}
	}
	for _, in := range []string{strings.Repeat("遥", MaxOwnerNameRunes+1), "温\n遥", "温\t遥", "温\x00遥"} {
		if _, err := SaveOwnerName(ctx, store, in, at); !errors.Is(err, core.ErrOwnerNameInvalid) {
			t.Fatalf("save %q err = %v, want ErrOwnerNameInvalid", in, err)
		}
	}
	if len(store.saved) != 4 {
		t.Fatalf("only valid names reach the store: %q", store.saved)
	}
}
