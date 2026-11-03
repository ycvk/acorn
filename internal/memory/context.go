package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/ycvk/acorn/internal/core"
)

type History struct {
	Messages  []core.SessionMessageRecord
	Summary   *core.ThreadSummary
	SourceIDs []string
	Epoch     int64
	Tokens    int
}

// History selects complete recent messages and incrementally summarizes the
// older prefix. through binds a run to its input even when another turn
// arrives; after, when positive, leaves out the messages up to it and the
// summary, so a run sees only its own input.
func (e *Engine) History(ctx context.Context, session string, after, through int64, budget int) (History, error) {
	out := History{}
	epoch, err := e.cfg.Store.MemoryEpoch(ctx)
	if err != nil {
		return out, err
	}
	out.Epoch = epoch
	if budget <= 0 {
		return out, errors.New("history budget must be positive")
	}
	summaryBudget := threadSummaryBudget(budget)
	recentBudget := budget - summaryBudget
	end := through
	for {
		page, cursor, err := e.cfg.Store.MemoryMessages(ctx, session, after, end, true, 128)
		if err != nil {
			return out, err
		}
		stop := false
		for _, m := range page {
			n, err := e.cfg.Count(ctx, m.Content)
			if err != nil {
				return out, err
			}
			n += 64 + captureImageTokens(m)
			if len(out.Messages) == 0 && n <= budget {
				summaryBudget = min(summaryBudget, budget-n)
				recentBudget = budget - summaryBudget
			}
			if out.Tokens+n > recentBudget {
				if len(out.Messages) == 0 {
					return out, fmt.Errorf("current input requires %d tokens; history budget is %d", n, recentBudget)
				}
				stop = true
				break
			}
			out.Messages = append(out.Messages, m)
			out.Tokens += n
		}
		if stop || cursor <= after+1 {
			break
		}
		end = cursor - 1
	}
	slices.Reverse(out.Messages)
	if after == 0 && len(out.Messages) > 0 && summaryBudget >= minThreadSummaryTokens {
		cutoff := out.Messages[0].ID - 1
		out.Summary, err = e.summarizeThrough(ctx, session, cutoff, epoch, summaryBudget)
		if err != nil {
			return out, err
		}
	}
	if out.Summary != nil {
		out.SourceIDs = append(out.SourceIDs, out.Summary.SourceIDs...)
		n, err := e.cfg.Count(ctx, out.Summary.Content)
		if err != nil {
			return out, err
		}
		out.Tokens += n + 64
	}
	for _, m := range out.Messages {
		out.SourceIDs = append(out.SourceIDs, core.MessageSourceID(m.ID))
	}
	latest, err := e.cfg.Store.MemoryEpoch(ctx)
	if err != nil {
		return out, err
	}
	if latest != out.Epoch {
		return out, core.ErrMemoryExcluded
	}
	return out, nil
}

// threadSummaryBudget is the share of a history budget reserved for the
// thread summary: an eighth of the budget within [1024, 8192], so a larger
// history budget carries a fuller summary of decisions, conditions and
// speakers. Below 8192 the summary takes at most a quarter, capped at 1024.
func threadSummaryBudget(budget int) int {
	if budget < 8192 {
		return min(1024, budget/4)
	}
	return min(8192, max(1024, budget/8))
}

// minThreadSummaryTokens is the smallest summary budget that holds the
// summary's sections; with less, no summary is written.
const minThreadSummaryTokens = 512

// threadSummaryAttempts is how many times a thread summary is asked for
// before a reply that misses its sections fails history.
const threadSummaryAttempts = 2

func (e *Engine) summarizeThrough(ctx context.Context, session string, through, epoch int64, budget int) (*core.ThreadSummary, error) {
	if through <= 0 {
		return nil, nil
	}
	summary, err := e.cfg.Store.LoadThreadSummary(ctx, session)
	if err != nil {
		return nil, err
	}
	if summary != nil && summary.ThroughMessageID > through {
		return nil, fmt.Errorf("thread summary cursor %d is newer than run history %d", summary.ThroughMessageID, through)
	}
	var cursor int64
	if summary != nil {
		cursor = summary.ThroughMessageID
	}
	instruction := threadSummaryInstruction(budget - 64)
pages:
	for cursor < through {
		page, next, err := e.cfg.Store.MemoryMessages(ctx, session, cursor, through, false, 64)
		if err != nil {
			return nil, err
		}
		if next == 0 {
			break
		}
		if len(page) == 0 {
			cursor = next
			continue
		}
		previous := ""
		var sources []string
		if summary != nil {
			previous = summary.Content
			sources = append(sources, summary.SourceIDs...)
		}
		prefix := previousSummary(previous)
		var texts []string
		processed := cursor
		for _, m := range page {
			line := fmt.Sprintf("source=message:%d speaker=%s\n%s", m.ID, m.Role, m.Content)
			candidate := prefix + "\n" + strings.Join(append(append([]string(nil), texts...), line), "\n")
			n, err := e.cfg.Count(ctx, instruction+candidate)
			if err != nil {
				return nil, err
			}
			if n > BatchTokens-128 {
				if len(texts) == 0 {
					sources = append(sources, core.MessageSourceID(m.ID))
					summary, err = e.summarizeStep(ctx, session, m.ID, sources, epoch, budget, func(sources []string, budget int) (string, error) {
						return e.summarizeLargeMessage(ctx, previous, m, sources, epoch, budget)
					})
					if err != nil {
						return nil, err
					}
					cursor = m.ID
					continue pages
				}
				break
			}
			texts = append(texts, line)
			sources = append(sources, core.MessageSourceID(m.ID))
			processed = m.ID
		}
		input := prefix + "\n" + strings.Join(texts, "\n")
		summary, err = e.summarizeStep(ctx, session, processed, sources, epoch, budget, func(sources []string, budget int) (string, error) {
			return e.summarizeInput(ctx, input, sources, epoch, budget)
		})
		if err != nil {
			return nil, err
		}
		cursor = processed
	}
	return summary, nil
}

// summarizeStep saves the thread summary through a message: the model's
// summary within what the files sections leave of budget, then the files
// sections, whose call sources join the summary's sources.
func (e *Engine) summarizeStep(ctx context.Context, session string, through int64, sources []string, epoch int64, budget int, summarize func(sources []string, budget int) (string, error)) (*core.ThreadSummary, error) {
	files, fileSources, tokens, err := e.summaryFiles(ctx, session, through, budget)
	if err != nil {
		return nil, err
	}
	sources = slices.Clone(sources)
	for _, id := range fileSources {
		if !slices.Contains(sources, id) {
			sources = append(sources, id)
		}
	}
	content, err := summarize(sources, budget-tokens)
	if err != nil {
		return nil, err
	}
	summary := &core.ThreadSummary{SessionID: session, ThroughMessageID: through, Content: content + files, SourceIDs: sources, Epoch: epoch, UpdatedAt: e.cfg.Clock()}
	if err := e.cfg.Store.SaveThreadSummary(ctx, *summary); err != nil {
		return nil, err
	}
	return summary, nil
}

// summaryFiles renders the files sections of a thread summary through a
// message, with their token count, from the files and send_file calls of the
// summarized runs that succeeded with a JSON object, and lists those calls'
// sources. The sections take at most a quarter of budget; past it the least
// recently touched files are left out first.
func (e *Engine) summaryFiles(ctx context.Context, session string, through int64, budget int) (string, []string, int, error) {
	calls, err := e.cfg.Store.ThreadToolCalls(ctx, session, through, fileTools)
	if err != nil {
		return "", nil, 0, err
	}
	var touches FileTouches
	var sources []string
	for _, call := range calls {
		touch, ok := TouchedFile(call.Name, call.Arguments)
		if !ok || !call.ObjectResult {
			continue
		}
		touches = touches.Touch(touch)
		sources = append(sources, call.SourceID)
	}
	for omitted := 0; omitted <= len(touches); omitted++ {
		text := "\n\n" + touches.sections(omitted)
		n, err := e.cfg.Count(ctx, text)
		if err != nil {
			return "", nil, 0, err
		}
		if n <= budget/4 {
			return text, sources, n, nil
		}
	}
	return "", nil, 0, fmt.Errorf("thread summary files sections exceed %d tokens", budget/4)
}

// RefreshRecall revalidates selected records without repeating an external query.
func (e *Engine) RefreshRecall(ctx context.Context, prior core.MemoryRecall) (core.MemoryRecall, error) {
	out := prior
	out.Hits = nil
	out.Sources = nil
	for _, hit := range prior.Hits {
		records, err := e.cfg.Store.MemoryRecordsByIDs(ctx, []string{hit.Record.ID}, core.MemoryQuery{Mode: "current", AsOf: e.cfg.Clock(), Limit: 1})
		if err != nil {
			return out, err
		}
		if len(records) > 0 {
			hit.Record = records[0]
			out.Hits = append(out.Hits, hit)
		}
	}
	for _, source := range prior.Sources {
		s, err := e.cfg.Store.LoadMemorySource(ctx, source.ID)
		if err == nil {
			out.Sources = append(out.Sources, s)
		} else if !errors.Is(err, core.ErrMemoryExcluded) {
			return out, err
		}
	}
	epoch, err := e.cfg.Store.MemoryEpoch(ctx)
	if err != nil {
		return out, err
	}
	out.Epoch = epoch
	data, err := json.Marshal(out)
	if err != nil {
		return out, err
	}
	out.Tokens, err = e.cfg.Count(ctx, string(data))
	return out, err
}

// summarizeLargeMessage summarizes a message too large for one call in
// fragments, each updating the summary of the ones before it.
func (e *Engine) summarizeLargeMessage(ctx context.Context, previous string, m core.SessionMessageRecord, sources []string, epoch int64, budget int) (string, error) {
	instruction := threadSummaryInstruction(budget - 64)
	runes := []rune(m.Content)
	for offset := 0; offset < len(runes); {
		header := fmt.Sprintf("%s\nsource=message:%d speaker=%s fragment_at=%d\n", previousSummary(previous), m.ID, m.Role, offset)
		overhead, err := e.cfg.Count(ctx, instruction+header)
		if err != nil {
			return "", err
		}
		chunk, next, err := e.fitPrefix(ctx, runes, offset, BatchTokens-overhead-128)
		if err != nil {
			return "", err
		}
		if previous, err = e.summarizeInput(ctx, header+chunk, sources, epoch, budget); err != nil {
			return "", err
		}
		offset = next
	}
	return previous, nil
}

// previousSummary marks the summary a thread summary call updates.
func previousSummary(content string) string {
	if content == "" {
		return ""
	}
	return "<previous_summary>\n" + content + "\n</previous_summary>"
}

// summarizeInput asks for a thread summary of input that fits budget,
// recording input's sources against the current run first. A reply that
// misses the summary's sections is asked for again, up to
// threadSummaryAttempts times.
func (e *Engine) summarizeInput(ctx context.Context, input string, sources []string, epoch int64, budget int) (string, error) {
	if scope := scopeFrom(ctx); scope.runID != "" {
		sum := sha256.Sum256([]byte(input))
		if err := e.cfg.Store.SaveMemorySnapshotRefs(ctx, hex.EncodeToString(sum[:]), scope.runID, sources, nil, epoch); err != nil {
			return "", err
		}
	}
	limit := budget - 64
	instruction := threadSummaryInstruction(limit)
	var content string
	for attempt := 1; content == ""; attempt++ {
		reply, err := e.generate(ctx, "thread_summary", instruction, input, limit)
		if err != nil {
			return "", err
		}
		if err = CheckSummary(reply); err == nil {
			content = reply
		} else if attempt == threadSummaryAttempts {
			return "", fmt.Errorf("thread summary: %w", err)
		}
	}
	n, err := e.cfg.Count(ctx, content)
	if err != nil {
		return "", err
	}
	if n+64 > budget {
		return "", fmt.Errorf("thread summary exceeds %d-token budget", budget)
	}
	return content, nil
}

// captureImageTokens is what the images a capture message carries to the model
// add to its size.
func captureImageTokens(m core.SessionMessageRecord) int {
	if m.Role != core.MessageRoleCapture {
		return 0
	}
	return core.ImageInputTokens * len(core.CaptureImagePaths(m.Content))
}
