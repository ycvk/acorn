package app

import (
	"sync"
	"sync/atomic"

	"github.com/ycvk/acorn/internal/runtime"
)

type clientRunStartSignal struct {
	started     chan struct{}
	failed      chan error
	closeOnce   sync.Once
	failureOnce sync.Once
	hasStarted  atomic.Bool
}

func newRunStartSignal() *clientRunStartSignal {
	return &clientRunStartSignal{
		started: make(chan struct{}),
		failed:  make(chan error, 1),
	}
}

func (s *clientRunStartSignal) Started() <-chan struct{} {
	return s.started
}

func (s *clientRunStartSignal) Failed() <-chan error {
	return s.failed
}

func (s *clientRunStartSignal) Sink(item runtime.StreamItem) error {
	if item.Kind == runtime.StreamKindRunStarted {
		s.hasStarted.Store(true)
		s.closeOnce.Do(func() { close(s.started) })
	}
	return nil
}

func (s *clientRunStartSignal) MarkFailed(err error) bool {
	if err == nil || s.hasStarted.Load() {
		return false
	}
	s.failureOnce.Do(func() { s.failed <- err })
	return true
}
