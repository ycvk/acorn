package api

import (
	"context"
	"errors"
	"testing"

	"github.com/ycvk/acorn/internal/core"
)

type eventLoadFailureStore struct {
	unimplementedStore
	err error
}

func (s eventLoadFailureStore) LoadRun(context.Context, string) (*core.RunRecord, error) {
	return &core.RunRecord{RunID: "run_1"}, nil
}

func (s eventLoadFailureStore) LoadEvents(context.Context, string) ([]core.EventRecord, error) {
	return nil, s.err
}

func (s eventLoadFailureStore) LoadEventsAfter(context.Context, string, int64) ([]core.EventRecord, error) {
	return nil, s.err
}

func TestEventServiceReportsStoreFailuresAsStoreErrors(t *testing.T) {
	storeErr := errors.New("disk I/O error")
	service := NewEventService(eventLoadFailureStore{err: storeErr}, nil)

	_, afterErr := service.LoadRunEventsAfter(context.Background(), "run_1", 0)
	_, detailErr := service.LoadRunEventsForDetail(context.Background(), "run_1")
	for name, err := range map[string]error{"after": afterErr, "detail": detailErr} {
		if !errors.Is(err, storeErr) {
			t.Fatalf("%s err = %v, want the store error", name, err)
		}
		if errors.Is(err, ErrClientProjectionFailed) {
			t.Fatalf("%s err = %v, reported as a projection failure", name, err)
		}
	}
}

func TestModelUsageIsDiagnostic(t *testing.T) {
	if IsLiveRunEventKind("model.usage") {
		t.Fatal("usage must not change the mobile live contract")
	}
}
