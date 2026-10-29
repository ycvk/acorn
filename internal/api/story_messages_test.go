package api

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/ycvk/acorn/internal/core"
)

// storyMessagesFake has story 41, which the newest briefing listed under
// briefed, and records the messages sent from it in thread_story.
type storyMessagesFake struct {
	briefed string
	writes  []core.StoryMessageWrite
	busy    bool
}

var storyFixture = core.NewsStory{ID: 41, Section: "国际", Title: "菲律宾央行加息", Summary: "加息 25 个基点", Items: []core.NewsStoryItem{
	{WatchItem: core.WatchItem{Title: "菲律宾央行意外加息", URL: "https://bbc.test/b1", SeenAt: nowTestTime}, Source: "BBC 中文"},
}}

func (f *storyMessagesFake) NewsStory(_ context.Context, id int64) (core.NewsStory, error) {
	if id != storyFixture.ID {
		return core.NewsStory{}, fmt.Errorf("%w: %d", core.ErrNewsStoryNotFound, id)
	}
	return storyFixture, nil
}

func (f *storyMessagesFake) BriefingStoryTitle(context.Context, int64) (string, error) {
	return f.briefed, nil
}

func (f *storyMessagesFake) SendStoryMessage(_ context.Context, in core.StoryMessageWrite) (core.SessionMessageRecord, error) {
	if f.busy {
		return core.SessionMessageRecord{}, fmt.Errorf("%w: thread_story", core.ErrThreadBusy)
	}
	f.writes = append(f.writes, in)
	return core.SessionMessageRecord{ID: int64(len(f.writes)), SessionID: "thread_story", TurnIndex: len(f.writes), Role: "user", Content: in.Text}, nil
}

func TestStoryMessagesStartARunInTheStorysConversation(t *testing.T) {
	stub := &clientHandlerStub{}
	threads, runs, events := newClientHotPathServices(stub)
	stories := &storyMessagesFake{}
	threads.stories = stories
	server := &Server{threads: threads, runs: runs, events: events, deviceAuth: newDeviceAuthTestService(&deviceAuthHandlerStub{}), logger: slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))}
	router := chi.NewRouter()
	server.registerRoutes(router)

	// The message and the conversation take the title the owner saw on the
	// card, else the story's own.
	for _, tc := range []struct {
		path    string
		briefed string
		intent  core.StoryIntent
		title   string
		text    string
	}{
		{"/v1/news/stories/41:discuss", "菲律宾央行意外加息", core.StoryDiscuss, "菲律宾央行意外加息", "展开讲讲：菲律宾央行意外加息"},
		{"/v1/news/stories/41:follow", "菲律宾央行意外加息", core.StoryFollow, "菲律宾央行意外加息", "持续跟进：菲律宾央行意外加息"},
		{"/v1/news/stories/41:discuss", "", core.StoryDiscuss, "菲律宾央行加息", "展开讲讲：菲律宾央行加息"},
	} {
		stub.run = Run{}
		stories.briefed = tc.briefed
		rec := performClientRequest(router, http.MethodPost, tc.path, "")
		if rec.Code != http.StatusCreated {
			t.Fatalf("%s status = %d body=%s", tc.path, rec.Code, rec.Body.String())
		}
		var got StoryMessageResponse
		decodeClientTestJSON(t, rec, &got)
		if got.ThreadID != "thread_story" || got.Text != tc.text || got.Run.ID == "" || got.Run.ThreadID != "thread_story" {
			t.Fatalf("%s response = %+v", tc.path, got)
		}
		if stub.createRunThreadID != "thread_story" {
			t.Fatalf("%s started a run in %q", tc.path, stub.createRunThreadID)
		}
		write := stories.writes[len(stories.writes)-1]
		if write.Intent != tc.intent || write.Text != tc.text || write.Title != tc.title || write.NewThreadID == "" || !reflect.DeepEqual(write.Story, core.SnapshotOf(storyFixture)) {
			t.Fatalf("%s wrote %+v", tc.path, write)
		}
	}

	stories.busy = true
	for _, tc := range []struct {
		path   string
		status int
		code   string
	}{
		{"/v1/news/stories/41:follow", http.StatusConflict, "thread_busy"},
		{"/v1/news/stories/7:discuss", http.StatusNotFound, "story_not_found"},
		{"/v1/news/stories/x:discuss", http.StatusBadRequest, "story_id must be a positive integer"},
	} {
		rec := performClientRequest(router, http.MethodPost, tc.path, "")
		if rec.Code != tc.status || !strings.Contains(rec.Body.String(), tc.code) {
			t.Errorf("%s: status = %d body=%s", tc.path, rec.Code, rec.Body.String())
		}
	}
	if rec := performClientRequestWithoutAuth(router, http.MethodPost, "/v1/news/stories/41:discuss", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated status = %d", rec.Code)
	}
}
