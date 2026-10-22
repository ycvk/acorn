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

// consolidateBatchRecords bounds how many changed records share one
// consolidation call; facts committed together are reconciled together.
const consolidateBatchRecords = 8

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
	changed, batch, err := e.consolidationBatch(ctx, job, read.Record)
	if err != nil {
		return err
	}
	if err := e.consolidateRecords(ctx, job, batch, changed); err != nil {
		return errors.Join(err, e.recoverMemoryJobs(ctx, batch, err))
	}
	return nil
}

// consolidationBatch claims further ready consolidation jobs whose current
// records fit the model input beside the leased record.
func (e *Engine) consolidationBatch(ctx context.Context, job core.MemoryJob, record core.MemoryRecord) ([]core.MemoryRecord, []core.MemoryJob, error) {
	now := e.cfg.Clock()
	ready, err := e.cfg.Store.ReadyMemoryJobs(ctx, now, "consolidate", 2*consolidateBatchRecords)
	if err != nil {
		return nil, nil, err
	}
	changed := []core.MemoryRecord{record}
	byJob := map[int64]core.MemoryRecord{}
	var ids []int64
	for _, candidate := range ready {
		if len(changed) == consolidateBatchRecords {
			break
		}
		read, err := e.cfg.Store.ReadMemory(ctx, candidate.ObjectID)
		if errors.Is(err, core.ErrMemoryExcluded) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		if read.Record.ID == record.ID || fmt.Sprint(read.Record.Revision) != candidate.Version {
			continue
		}
		fits, err := e.consolidationFits(ctx, append(append([]core.MemoryRecord(nil), changed...), read.Record), nil)
		if err != nil {
			return nil, nil, err
		}
		if !fits {
			break
		}
		changed = append(changed, read.Record)
		byJob[candidate.ID] = read.Record
		ids = append(ids, candidate.ID)
	}
	if len(ids) == 0 {
		return changed, nil, nil
	}
	batch, err := e.cfg.Store.ClaimMemoryJobs(ctx, ids, now, memoryJobLease)
	if err != nil {
		return nil, nil, err
	}
	changed = changed[:1]
	for _, claimed := range batch {
		changed = append(changed, byJob[claimed.ID])
	}
	return changed, batch, nil
}

func (e *Engine) consolidationFits(ctx context.Context, changed, related []core.MemoryRecord) (bool, error) {
	data, err := json.Marshal(consolidationInput{Changed: changed, Related: related})
	if err != nil {
		return false, err
	}
	n, err := e.cfg.Count(ctx, consolidateInstruction+string(data))
	if err != nil {
		return false, err
	}
	return n <= BatchTokens-128, nil
}

type consolidationInput struct {
	Changed []core.MemoryRecord `json:"changed"`
	Related []core.MemoryRecord `json:"related"`
}

func (e *Engine) consolidateRecords(ctx context.Context, job core.MemoryJob, batch []core.MemoryJob, changed []core.MemoryRecord) error {
	ids := map[string]bool{}
	seedIDs := make([]string, 0, len(changed))
	needsReview := false
	for _, r := range changed {
		ids[r.ID] = true
		seedIDs = append(seedIDs, r.ID)
		needsReview = needsReview || r.NeedsReview
	}
	neighbors, err := e.cfg.Store.MemoryNeighbors(ctx, seedIDs, 30)
	if err != nil {
		return err
	}
	routes := [][]core.MemoryRecord{neighbors}
	names := []string{"entity"}
	for _, r := range changed {
		semantic, err := e.semanticCandidates(ctx, core.MemoryQuery{Query: r.Content + "\n" + r.Scope, Mode: "current", AsOf: e.cfg.Clock(), Limit: 20}, e.cfg.Index)
		if err != nil {
			return err
		}
		lexical, err := e.cfg.Store.SearchMemoryText(ctx, core.MemoryQuery{Query: r.Content, Limit: 20})
		if err != nil {
			return err
		}
		routes = append(routes, semantic, lexical)
		names = append(names, "semantic", "keyword")
	}
	// Fuse the routes before fitting the shared model input budget so a
	// large entity neighborhood cannot crowd out semantic evidence.
	var related []core.MemoryRecord
	for _, hit := range fuseMemory(routes, names) {
		r := hit.Record
		if ids[r.ID] {
			continue
		}
		fits, err := e.consolidationFits(ctx, changed, append(append([]core.MemoryRecord(nil), related...), r))
		if err != nil {
			return err
		}
		if !fits {
			break
		}
		related = append(related, r)
		ids[r.ID] = true
	}
	if len(changed)+len(related) < 2 && !needsReview {
		return e.cfg.Store.FinishMemoryJob(ctx, job, "awaiting related evidence", e.cfg.Clock())
	}
	records := append(append([]core.MemoryRecord(nil), changed...), related...)
	input, err := json.Marshal(consolidationInput{Changed: changed, Related: related})
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
	byID := make(map[string]core.MemoryRecord, len(records))
	for _, r := range records {
		byID[r.ID] = r
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
		if c.Merges != "" {
			if err := validateMerge(c, byID); err != nil {
				return err
			}
		}
		originalParents := byID[c.Draft.ID].Parents
		for _, p := range c.Draft.Parents {
			if !ids[p] && !slices.Contains(originalParents, p) {
				return errors.New("consolidation references an unseen parent")
			}
		}
		if c.Draft.Kind == "insight" {
			owners, err := e.independentOwnerSources(ctx, byID, c.Draft.Parents)
			if err != nil {
				return err
			}
			if owners < 2 {
				return errors.New("consolidation insight needs two independent owner sources")
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
	_, err = e.cfg.Store.CommitMemory(ctx, core.MemoryMutation{Changes: output.Changes, Links: output.Links, Lease: &job, Batch: batch, Epoch: job.Epoch, Now: e.cfg.Clock()})
	return err
}

// validateMerge checks a merge against the records the model was shown. The
// code picks the survivor: a pinned fact over an unpinned one, otherwise the
// earlier recorded one; the draft must be that record at its current revision
// and carry every piece of the duplicate's evidence.
func validateMerge(c core.MemoryChange, byID map[string]core.MemoryRecord) error {
	if c.Supersedes != "" {
		return errors.New("a consolidation change either merges or supersedes")
	}
	survivor, ok := byID[c.Draft.ID]
	if !ok || c.Draft.ID == "" {
		return errors.New("consolidation merges into an unseen record")
	}
	merged, ok := byID[c.Merges]
	if !ok {
		return errors.New("consolidation merges an unseen record")
	}
	if survivor.ID == merged.ID {
		return errors.New("consolidation merges a record into itself")
	}
	for _, r := range []core.MemoryRecord{survivor, merged} {
		if r.Kind != "fact" || r.State != "current" {
			return fmt.Errorf("consolidation merges only current facts, %s is a %s %s", r.ID, r.State, r.Kind)
		}
	}
	if c.ExpectedRevision != survivor.Revision || c.MergesRevision != merged.Revision {
		return errors.New("consolidation merge revisions do not match the supplied records")
	}
	if want := mergeSurvivor(survivor, merged); want.ID != survivor.ID {
		return fmt.Errorf("consolidation merge must keep %s, not %s", want.ID, survivor.ID)
	}
	if survivor.Pinned && !c.Draft.Pinned {
		return fmt.Errorf("consolidation merge unpins %s", survivor.ID)
	}
	for _, evidence := range merged.Evidence {
		if !slices.ContainsFunc(c.Draft.Evidence, func(e core.MemoryEvidence) bool {
			relation := e.Relation
			if relation == "" {
				relation = "supports"
			}
			return e.SourceID == evidence.SourceID && e.Quote == evidence.Quote && relation == evidence.Relation
		}) {
			return fmt.Errorf("consolidation merge drops evidence of %s", merged.ID)
		}
	}
	return nil
}

// mergeSurvivor is the record that survives a merge of a and b: the pinned
// one when exactly one is pinned, otherwise the one recorded first, ties
// broken by id.
func mergeSurvivor(a, b core.MemoryRecord) core.MemoryRecord {
	if a.Pinned != b.Pinned {
		if a.Pinned {
			return a
		}
		return b
	}
	if a.RecordedAt.Before(b.RecordedAt) || (a.RecordedAt.Equal(b.RecordedAt) && a.ID < b.ID) {
		return a
	}
	return b
}

// independentOwnerSources counts the distinct owner sources behind parents,
// resolved through each parent's root sources. Parents outside the model
// input are an existing insight's retained parents.
func (e *Engine) independentOwnerSources(ctx context.Context, byID map[string]core.MemoryRecord, parents []string) (int, error) {
	seen := map[string]bool{}
	owners := 0
	for _, id := range parents {
		parent, ok := byID[id]
		if !ok {
			read, err := e.cfg.Store.ReadMemory(ctx, id)
			if err != nil {
				return 0, err
			}
			parent = read.Record
		}
		for _, sourceID := range parent.SourceIDs {
			if seen[sourceID] {
				continue
			}
			seen[sourceID] = true
			source, err := e.cfg.Store.LoadMemorySource(ctx, sourceID)
			if err != nil {
				return 0, err
			}
			if source.Speaker == "owner" {
				owners++
			}
		}
	}
	return owners, nil
}
