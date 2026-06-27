package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"github.com/ycvk/acorn/internal/memory"
)

// Reviewer periodically reviews completed runs and distills durable facts
// into the memory store. It is Layer 3 of the three-layer memory
// architecture (ADR-0002): Active Memory (always-on snapshot) + Archive
// (append-only history) + Periodic Review (this).
//
// Every interval completed runs trigger one LLM call that examines
// the recent run outputs and decides what's worth remembering long-term.
// The LLM produces structured facts which are persisted via the memory
// service's CreateFact. Review is asynchronous — it never blocks run
// finalization.
type Reviewer struct {
	memory        memory.Service
	newChatModel  func(ctx context.Context) (einomodel.BaseChatModel, error)
	interval      int // 0 = disabled
	mu            sync.Mutex
	completedRuns int
	pendingInputs []runSummary
}

type runSummary struct {
	runID  string
	input  string
	output string
}

// NewReviewer constructs a periodic memory reviewer. interval is the number
// of completed runs between reviews (0 disables). newChatModel is the
// model factory — wire injects NewChatModelWithModel so review can run on a
// cheaper model (memory.review.review_model) without the reviewer knowing
// about config.
func NewReviewer(mem memory.Service, newChatModel func(ctx context.Context) (einomodel.BaseChatModel, error), interval int) *Reviewer {
	if interval <= 0 {
		return nil
	}
	return &Reviewer{
		memory:       mem,
		newChatModel: newChatModel,
		interval:     interval,
	}
}

// RecordRun is called by the Executor after each run completes. When the
// counter reaches the interval, a review is triggered asynchronously.
func (r *Reviewer) RecordRun(runID, input, output string) {
	if r == nil || r.interval <= 0 {
		return
	}
	r.mu.Lock()
	r.completedRuns++
	r.pendingInputs = append(r.pendingInputs, runSummary{runID: runID, input: input, output: output})
	threshold := r.completedRuns >= r.interval
	pending := r.pendingInputs
	if threshold {
		r.completedRuns = 0
		r.pendingInputs = nil
	}
	r.mu.Unlock()

	if threshold {
		go r.review(context.Background(), pending)
	}
}

func (r *Reviewer) review(ctx context.Context, runs []runSummary) {
	if r.memory == nil || len(runs) == 0 {
		return
	}

	// Inject existing fact titles so the LLM avoids duplicating them.
	facts, err := r.memory.ListFacts(ctx, memory.RecordSelection{})
	if err != nil {
		slog.Warn("memory review: list facts failed, proceeding without dedup", "err", err)
		facts = nil
	}
	var knownTitles []string
	for _, f := range facts {
		if f.Status != memory.StatusRetired {
			knownTitles = append(knownTitles, f.Title)
		}
	}

	var b strings.Builder
	b.WriteString("You are reviewing recent agent runs to extract durable facts worth remembering long-term.\n\n")
	if len(knownTitles) > 0 {
		b.WriteString("Facts already in memory (do not duplicate these):\n")
		for _, t := range knownTitles {
			b.WriteString(fmt.Sprintf("- %s\n", t))
		}
		b.WriteString("\n")
	}
	b.WriteString("For each fact worth keeping, output a line:\nFACT: <title> | <body>\n\n")
	b.WriteString("Rules:\n")
	b.WriteString("- Only extract facts that are stable, reusable, and non-obvious.\n")
	b.WriteString("- Skip ephemeral run details, temporary states, or trivial observations.\n")
	b.WriteString("- Skip facts already in memory (listed above).\n")
	b.WriteString("- If nothing is worth remembering, output \"NOTHING\".\n")
	b.WriteString("- Keep each fact body under 200 characters.\n\n")
	b.WriteString("Recent runs:\n")
	for _, run := range runs {
		b.WriteString(fmt.Sprintf("\n[Run %s]\nInput: %s\nOutput: %s\n", run.runID, truncateRunes(run.input, 500), truncateRunes(run.output, 500)))
	}

	model, err := r.newChatModel(ctx)
	if err != nil {
		slog.Warn("memory review: model creation failed", "err", err)
		return
	}
	resp, err := model.Generate(ctx, []*schema.Message{schema.UserMessage(b.String())})
	if err != nil {
		slog.Warn("memory review: LLM call failed", "err", err)
		return
	}
	if resp == nil {
		return
	}

	// Parse FACT: <title> | <body> lines and persist them.
	factList := parseFacts(resp.Content)
	for _, f := range factList {
		_, err := r.memory.CreateFact(ctx, memory.CreateFactRequest{
			Title: f.title,
			Body:  f.body,
			Tags:  []string{"review"},
		})
		if err != nil {
			slog.Warn("memory review: create fact failed", "title", f.title, "err", err)
		} else {
			slog.Info("memory review: persisted fact", "title", f.title)
		}
	}
}

type parsedFact struct {
	title string
	body  string
}

func parseFacts(text string) []parsedFact {
	var facts []parsedFact
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "FACT:") {
			continue
		}
		rest := strings.TrimSpace(strings.TrimPrefix(line, "FACT:"))
		parts := strings.SplitN(rest, "|", 2)
		if len(parts) != 2 {
			continue
		}
		title := strings.TrimSpace(parts[0])
		body := strings.TrimSpace(parts[1])
		if title == "" || body == "" {
			continue
		}
		facts = append(facts, parsedFact{title: title, body: body})
	}
	return facts
}

// truncateRunes is the shared rune-safe truncation used by both the
// reviewer (run summaries) and the history fallback summary.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "..."
}
