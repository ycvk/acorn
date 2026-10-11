package memory

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/ycvk/acorn/internal/core"
)

func (e *Engine) embedRecord(ctx context.Context, job core.MemoryJob) error {
	read, err := e.cfg.Store.ReadMemory(ctx, job.ObjectID)
	if errors.Is(err, core.ErrMemoryExcluded) {
		return e.cfg.Store.FinishMemoryJob(ctx, job, "excluded record", e.cfg.Clock())
	}
	if err != nil {
		return err
	}
	record := read.Record
	if fmt.Sprint(record.Revision) != job.Version {
		found := false
		for _, revision := range read.Versions {
			if fmt.Sprint(revision.Revision) == job.Version {
				record, found = revision, true
				break
			}
		}
		if !found {
			return e.cfg.Store.FinishMemoryJob(ctx, job, "revision evidence is excluded", e.cfg.Clock())
		}
	}
	index, err := e.cfg.Store.MemoryIndex(ctx)
	if err != nil {
		return err
	}
	if index.Model != e.cfg.Index.Model || index.Dimensions != e.cfg.Index.Dimensions {
		return core.ErrMemoryIndexMismatch
	}
	vectors, err := e.embed(ctx, []string{record.Content + "\nScope: " + record.Scope}, "document")
	if err != nil {
		return err
	}
	values := make([]float32, len(vectors[0]))
	for i, v := range vectors[0] {
		values[i] = float32(v)
	}
	return e.cfg.Store.SaveMemoryVectors(ctx, job, []core.MemoryVector{{ID: record.ID, Revision: record.Revision, Generation: index.Generation, Values: values}}, e.cfg.Clock())
}

func (e *Engine) consolidate(ctx context.Context, job core.MemoryJob) error {
	read, err := e.cfg.Store.ReadMemory(ctx, job.ObjectID)
	if errors.Is(err, core.ErrMemoryExcluded) {
		return e.cfg.Store.FinishMemoryJob(ctx, job, "excluded record", e.cfg.Clock())
	}
	if err != nil {
		return err
	}
	if fmt.Sprint(read.Record.Revision) != job.Version {
		return e.cfg.Store.FinishMemoryJob(ctx, job, "record revision has advanced", e.cfg.Clock())
	}
	neighbors, err := e.cfg.Store.MemoryNeighbors(ctx, []string{read.Record.ID}, 30)
	if err != nil {
		return err
	}
	lexical, err := e.cfg.Store.SearchMemoryText(ctx, core.MemoryQuery{Query: read.Record.Content, Limit: 20})
	if err != nil {
		return err
	}
	semantic, err := e.semanticCandidates(ctx, core.MemoryQuery{Query: read.Record.Content + "\n" + read.Record.Scope, Mode: "current", AsOf: e.cfg.Clock(), Limit: 20}, e.cfg.Index)
	if err != nil {
		return err
	}
	// Fuse the three routes before fitting the shared model input budget so a
	// large entity neighborhood cannot crowd out semantic evidence.
	related := fuseMemory([][]core.MemoryRecord{semantic, neighbors, lexical}, []string{"semantic", "entity", "keyword"})
	records := []core.MemoryRecord{read.Record}
	ids := map[string]bool{read.Record.ID: true}
	for _, hit := range related {
		r := hit.Record
		if ids[r.ID] {
			continue
		}
		candidate := append(append([]core.MemoryRecord(nil), records...), r)
		data, err := json.Marshal(candidate)
		if err != nil {
			return err
		}
		n, err := e.cfg.Count(ctx, consolidateInstruction+string(data))
		if err != nil {
			return err
		}
		if n > e.cfg.BatchTokens-128 {
			break
		}
		records = candidate
		ids[r.ID] = true
	}
	if len(records) < 2 && !read.Record.NeedsReview {
		return e.cfg.Store.FinishMemoryJob(ctx, job, "awaiting related evidence", e.cfg.Clock())
	}
	input, err := json.Marshal(records)
	if err != nil {
		return err
	}
	reply, err := e.generate(ctx, "consolidate", consolidateInstruction, string(input), memoryJSONOutputTokens)
	if err != nil {
		return err
	}
	var output struct {
		Changes []core.MemoryChange `json:"changes"`
		Links   []core.MemoryLink   `json:"links"`
	}
	if err := decodeMemoryJSON(reply, &output); err != nil {
		return err
	}
	sources := map[string]bool{}
	for _, r := range records {
		for _, id := range r.SourceIDs {
			sources[id] = true
		}
	}
	for _, c := range output.Changes {
		if c.Draft.ID != "" && !ids[c.Draft.ID] {
			return errors.New("consolidation references an unseen record")
		}
		if c.Draft.ID == "" && c.Draft.Kind != "insight" && c.Supersedes == "" {
			return errors.New("consolidation creates sourced insights or explicit corrections")
		}
		if c.Supersedes != "" && !ids[c.Supersedes] {
			return errors.New("consolidation supersedes an unseen record")
		}
		var originalParents []string
		for _, r := range records {
			if r.ID == c.Draft.ID {
				originalParents = r.Parents
				break
			}
		}
		for _, p := range c.Draft.Parents {
			if !ids[p] && !slices.Contains(originalParents, p) {
				return errors.New("consolidation references an unseen parent")
			}
		}
		for _, evidence := range c.Draft.Evidence {
			if !sources[evidence.SourceID] {
				return errors.New("consolidation references unseen evidence")
			}
		}
	}
	for _, l := range output.Links {
		if !ids[l.FromID] || !ids[l.ToID] || !slices.Contains([]string{"contradicts", "related_to"}, l.Relation) {
			return errors.New("consolidation links must connect supplied records")
		}
	}
	_, err = e.cfg.Store.CommitMemory(ctx, core.MemoryMutation{Changes: output.Changes, Links: output.Links, Lease: &job, Epoch: job.Epoch, Now: e.cfg.Clock()})
	return err
}
