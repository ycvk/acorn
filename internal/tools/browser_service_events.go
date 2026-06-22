package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	cruntime "github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func (s *Service) handleEvent(ev any) {
	if s == nil {
		return
	}
	now := s.cfg.Now().Format(time.RFC3339Nano)
	switch event := ev.(type) {
	case *cruntime.EventConsoleAPICalled:
		s.recordConsoleEvent(event, now)
	case *network.EventResponseReceived:
		s.recordNetworkResponse(event, now)
	case *fetch.EventRequestPaused:
		s.handlePausedEvent(event)
	}
}

func (s *Service) recordConsoleEvent(event *cruntime.EventConsoleAPICalled, now string) {
	if event == nil {
		return
	}
	entry := ConsoleEntry{Type: string(event.Type), Timestamp: now}
	for _, arg := range event.Args {
		entry.Args = append(entry.Args, remoteObjectString(arg))
	}
	s.mu.Lock()
	if s.consoleEnabled {
		s.consoleEntries = append(s.consoleEntries, entry)
	}
	s.mu.Unlock()
}

func (s *Service) recordNetworkResponse(event *network.EventResponseReceived, now string) {
	if event == nil || event.Response == nil {
		return
	}
	entry := NetworkEntry{
		URL:               event.Response.URL,
		Status:            event.Response.Status,
		StatusText:        event.Response.StatusText,
		MimeType:          event.Response.MimeType,
		ResourceType:      string(event.Type),
		EncodedDataLength: event.Response.EncodedDataLength,
		Timestamp:         now,
	}
	s.mu.Lock()
	if s.networkEnabled {
		s.networkEntries = append(s.networkEntries, entry)
	}
	s.mu.Unlock()
}

func (s *Service) handlePausedEvent(event *fetch.EventRequestPaused) {
	if event.Request == nil {
		s.continueFetchRequest(event.RequestID)
		return
	}
	requestURL := strings.TrimSpace(event.Request.URL)
	if requestURL == "" {
		s.continueFetchRequest(event.RequestID)
		return
	}
	go s.handlePausedRequest(event.RequestID, requestURL)
}

func (s *Service) handlePausedRequest(requestID fetch.RequestID, requestURL string) {
	policyCtx, cancel := context.WithTimeout(context.Background(), s.cfg.Timeout)
	defer cancel()
	if !s.handlePausedRequestForTest(policyCtx, requestURL) {
		s.failFetchRequest(requestID)
		return
	}
	s.continueFetchRequest(requestID)
}

func (s *Service) continueFetchRequest(requestID fetch.RequestID) {
	browserCtx := s.currentContext()
	if browserCtx == nil {
		return
	}
	if err := chromedp.Run(browserCtx, fetch.ContinueRequest(requestID)); err != nil {
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("continue browser request %s: %v", requestID, err))
		s.mu.Unlock()
	}
}

func (s *Service) failFetchRequest(requestID fetch.RequestID) {
	browserCtx := s.currentContext()
	if browserCtx == nil {
		return
	}
	if err := chromedp.Run(browserCtx, fetch.FailRequest(requestID, network.ErrorReasonBlockedByClient)); err != nil {
		s.mu.Lock()
		s.policyErrors = append(s.policyErrors, fmt.Sprintf("fail browser request %s: %v", requestID, err))
		s.mu.Unlock()
	}
}

func (s *Service) handlePausedRequestForTest(ctx context.Context, requestURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(requestURL))
	if err != nil {
		s.recordPolicyError(fmt.Sprintf("browser blocked request %s: parse url: %v", requestURL, err))
		return false
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "about", "blob", "data":
		return true
	case "http", "https":
	default:
		s.recordPolicyError(fmt.Sprintf("browser blocked request %s: unsupported url scheme %q", requestURL, parsed.Scheme))
		return false
	}
	if _, err := s.cfg.Policy.Validate(ctx, requestURL); err != nil {
		s.recordPolicyError(fmt.Sprintf("browser blocked request %s: %v", requestURL, err))
		return false
	}
	return true
}

func (s *Service) recordPolicyError(message string) {
	s.mu.Lock()
	s.policyErrors = append(s.policyErrors, message)
	s.mu.Unlock()
}

func (s *Service) takePolicyError() string {
	if s == nil {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.policyErrors) == 0 {
		return ""
	}
	message := s.policyErrors[0]
	s.policyErrors = nil
	return message
}

func remoteObjectString(obj *cruntime.RemoteObject) string {
	if obj == nil {
		return ""
	}
	if len(obj.Value) > 0 {
		var value any
		if err := json.Unmarshal(obj.Value, &value); err == nil {
			return fmt.Sprint(value)
		}
	}
	if obj.Description != "" {
		return obj.Description
	}
	if obj.UnserializableValue != "" {
		return string(obj.UnserializableValue)
	}
	return string(obj.Type)
}
