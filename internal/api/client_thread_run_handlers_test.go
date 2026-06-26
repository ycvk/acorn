package api

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestThreadMessageRunHandlers(t *testing.T) {
	service := &clientHandlerStub{
		thread: Thread{
			ID:            "thread_1",
			Title:         "Inspect repo",
			WorkspaceRoot: "/repo",
			CreatedAt:     time.Date(2026, 5, 2, 10, 0, 0, 0, time.UTC),
			UpdatedAt:     time.Date(2026, 5, 2, 10, 1, 0, 0, time.UTC),
			State:         "new",
		},
		message: Message{
			ID:       "7",
			ThreadID: "thread_1",
			Role:     "user",
			Content: MessageContent{
				Type: "text",
				Text: "look around",
			},
			CreatedAt: time.Date(2026, 5, 2, 10, 2, 0, 0, time.UTC),
		},
		run: Run{
			ID:        "run_1",
			ThreadID:  "thread_1",
			Status:    "running",
			CreatedAt: time.Date(2026, 5, 2, 10, 3, 0, 0, time.UTC),
		},
	}
	router := newClientHandlerTestRouter(service)

	listThreads := performClientRequest(router, http.MethodGet, "/v1/threads", "")
	if listThreads.Code != http.StatusOK {
		t.Fatalf("list threads status = %d body=%s", listThreads.Code, listThreads.Body.String())
	}
	var threads ThreadListResponse
	decodeClientTestJSON(t, listThreads, &threads)
	if len(threads.Items) != 1 || threads.Items[0].ID != "thread_1" {
		t.Fatalf("unexpected threads response: %#v", threads)
	}

	createThread := performClientRequest(router, http.MethodPost, "/v1/threads", `{"title":" Inspect repo "}`)
	if createThread.Code != http.StatusCreated {
		t.Fatalf("create thread status = %d body=%s", createThread.Code, createThread.Body.String())
	}
	var thread ThreadDTO
	decodeClientTestJSON(t, createThread, &thread)
	if thread.ID != "thread_1" || thread.Title != "Inspect repo" || thread.WorkspaceRoot != "/repo" || thread.State != "new" {
		t.Fatalf("unexpected create thread response: %#v", thread)
	}
	if strings.Contains(createThread.Body.String(), "session_id") {
		t.Fatalf("thread response leaked session_id: %s", createThread.Body.String())
	}
	if service.createThreadTitle != "Inspect repo" {
		t.Fatalf("create thread title = %q, want trimmed title", service.createThreadTitle)
	}

	getThread := performClientRequest(router, http.MethodGet, "/v1/threads/thread_1", "")
	if getThread.Code != http.StatusOK {
		t.Fatalf("get thread status = %d body=%s", getThread.Code, getThread.Body.String())
	}

	updateThread := performClientRequest(router, http.MethodPatch, "/v1/threads/thread_1", `{"title":" New title "}`)
	if updateThread.Code != http.StatusOK {
		t.Fatalf("update thread status = %d body=%s", updateThread.Code, updateThread.Body.String())
	}
	if service.updateThreadID != "thread_1" || service.updateThreadTitle != "New title" {
		t.Fatalf("unexpected update request: id=%q title=%q", service.updateThreadID, service.updateThreadTitle)
	}

	deleteThread := performClientRequest(router, http.MethodDelete, "/v1/threads/thread_1", "")
	if deleteThread.Code != http.StatusNoContent {
		t.Fatalf("delete thread status = %d body=%s", deleteThread.Code, deleteThread.Body.String())
	}
	if service.deleteThreadID != "thread_1" {
		t.Fatalf("delete thread id = %q, want thread_1", service.deleteThreadID)
	}

	createMessage := performClientRequest(router, http.MethodPost, "/v1/threads/thread_1/messages", `{"content":{"type":"text","text":" look around "}}`)
	if createMessage.Code != http.StatusCreated {
		t.Fatalf("create message status = %d body=%s", createMessage.Code, createMessage.Body.String())
	}
	var message MessageDTO
	decodeClientTestJSON(t, createMessage, &message)
	if message.ID != "7" || message.ThreadID != "thread_1" || message.Content.Type != "text" || message.Content.Text != "look around" {
		t.Fatalf("unexpected create message response: %#v", message)
	}
	if service.createMessageThreadID != "thread_1" || service.createMessageContent != "look around" {
		t.Fatalf("unexpected create message call: thread=%q content=%q", service.createMessageThreadID, service.createMessageContent)
	}

	listMessages := performClientRequest(router, http.MethodGet, "/v1/threads/thread_1/messages", "")
	if listMessages.Code != http.StatusOK {
		t.Fatalf("list messages status = %d body=%s", listMessages.Code, listMessages.Body.String())
	}
	var messages MessageListResponse
	decodeClientTestJSON(t, listMessages, &messages)
	if len(messages.Items) != 1 || messages.Items[0].ID != "7" {
		t.Fatalf("unexpected messages response: %#v", messages)
	}

	createRun := performClientRequest(router, http.MethodPost, "/v1/threads/thread_1/runs", `{"skill_id":" skill.inspect "}`)
	if createRun.Code != http.StatusCreated {
		t.Fatalf("create run status = %d body=%s", createRun.Code, createRun.Body.String())
	}
	var run RunDTO
	decodeClientTestJSON(t, createRun, &run)
	if run.ID != "run_1" || run.ThreadID != "thread_1" || run.Status != "running" {
		t.Fatalf("unexpected create run response: %#v", run)
	}
	if service.createRunThreadID != "thread_1" || service.createRunSkillID != "skill.inspect" {
		t.Fatalf("unexpected create run call: thread=%q skill=%q", service.createRunThreadID, service.createRunSkillID)
	}

	getRun := performClientRequest(router, http.MethodGet, "/v1/runs/run_1", "")
	if getRun.Code != http.StatusOK {
		t.Fatalf("get run status = %d body=%s", getRun.Code, getRun.Body.String())
	}
}
