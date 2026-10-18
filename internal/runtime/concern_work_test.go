package runtime

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

type fakeWorkHost struct {
	dirs    []string
	deleted []string
	failure error
}

func (h *fakeWorkHost) WorkDirs(context.Context) ([]string, error) {
	return h.dirs, nil
}

func (h *fakeWorkHost) DeleteWorkDir(_ context.Context, id string) error {
	if h.failure != nil {
		return h.failure
	}
	h.deleted = append(h.deleted, id)
	return nil
}

type deletedThreadConcerns struct {
	core.MemoryStore
	ids []string
}

func (s deletedThreadConcerns) DeletedThreadConcernIDs(context.Context) ([]string, error) {
	return s.ids, nil
}

func TestRemoveForgottenWorkDeletesOnlyConcernsOfDeletedConversations(t *testing.T) {
	ctx := context.Background()
	concerns := deletedThreadConcerns{ids: []string{"m_gone", "m_never_worked_on"}}
	host := &fakeWorkHost{dirs: []string{"m_gone", "m_kept"}}
	if err := removeForgottenWork(ctx, concerns, host); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(host.deleted, []string{"m_gone"}) {
		t.Fatalf("deleted = %v, want only m_gone", host.deleted)
	}
	if err := removeForgottenWork(ctx, concerns, nil); err != nil {
		t.Fatalf("without a workstation: %v", err)
	}
	refused := errors.New("permission denied")
	host = &fakeWorkHost{dirs: []string{"m_gone"}, failure: refused}
	if err := removeForgottenWork(ctx, concerns, host); !errors.Is(err, refused) {
		t.Fatalf("failed delete err = %v", err)
	}
}
