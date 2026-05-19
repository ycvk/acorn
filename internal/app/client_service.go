package app

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrClientProjectionFailed = errors.New("client projection failed")
	ErrClientNoPendingMessage = errors.New("client pending user message not found")
	ErrClientInvalidRunMode   = errors.New("client run mode is invalid")
)

// ClientService owns the client-facing thread/message/run/event orchestration.
// It deliberately stays separate from ChatService because /v1 splits message
// creation, run creation, and event replay into distinct HTTP calls.
type ClientService struct {
	store         clientStore
	executors     executorFactory
	workspaceRoot string
	eventPoll     time.Duration
	newThreadID   func() string
	newRunID      func() string
}

func BuildClientService(store clientStore, executors executorFactory, workspaceRoot string) *ClientService {
	return &ClientService{
		store:         store,
		executors:     executors,
		workspaceRoot: workspaceRoot,
		eventPoll:     100 * time.Millisecond,
		newThreadID:   newThreadID,
		newRunID:      newRunID,
	}
}

func newThreadID() string {
	return fmt.Sprintf("session_%d", time.Now().UTC().UnixNano())
}

func newRunID() string {
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}
