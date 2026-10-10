package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

func (s *Store) MemoryIndex(ctx context.Context) (core.MemoryIndex, error) {
	return loadMemoryIndex(ctx, s.read)
}
func loadMemoryIndex(ctx context.Context, q memorySQL) (core.MemoryIndex, error) {
	var index core.MemoryIndex
	err := q.QueryRowContext(ctx, `SELECT generation,model,dimensions,state FROM memory_index_state WHERE id=1`).Scan(&index.Generation, &index.Model, &index.Dimensions, &index.State)
	if errors.Is(err, sql.ErrNoRows) {
		err = core.ErrMemoryNotFound
	}
	return index, err
}

func (s *Store) ConfigureMemoryIndex(ctx context.Context, want core.MemoryIndex, rebuild bool) (core.MemoryIndex, error) {
	if want.Model == "" || want.Dimensions <= 0 {
		return want, errors.New("memory index requires model and dimensions")
	}
	var result core.MemoryIndex
	err := s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		index, err := loadMemoryIndex(ctx, tx)
		if err == nil {
			if index.Model == want.Model && index.Dimensions == want.Dimensions && (!rebuild || index.State == "rebuilding") {
				result = index
				if index.State != "ready" && !rebuild {
					return fmt.Errorf("%w: index is %s", core.ErrMemoryIndexMismatch, index.State)
				}
				return nil
			}
			if !rebuild {
				return fmt.Errorf("%w: stored %s/%d requested %s/%d", core.ErrMemoryIndexMismatch, index.Model, index.Dimensions, want.Model, want.Dimensions)
			}
			var active int
			if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE status IN ('running','interrupted')`).Scan(&active); err != nil {
				return err
			}
			if active > 0 {
				return errors.New("memory index rebuild requires drained runs")
			}
			want.Generation = index.Generation + 1
			want.State = "rebuilding"
		} else if errors.Is(err, core.ErrMemoryNotFound) {
			want.Generation = 1
			want.State = "ready"
		} else {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO memory_index_state(id,generation,model,dimensions,state) VALUES(1,?,?,?,?) ON CONFLICT(id) DO UPDATE SET generation=excluded.generation,model=excluded.model,dimensions=excluded.dimensions,state=excluded.state`, want.Generation, want.Model, want.Dimensions, want.State); err != nil {
			return err
		}
		if rebuild {
			if _, err := tx.ExecContext(ctx, `INSERT INTO memory_jobs(operation,object_id,version,available_at,created_at) SELECT 'embed',v.record_id,CAST(v.revision AS TEXT),v.at,v.at FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id WHERE r.excluded=0 AND `+memoryRevisionVisibility+` ON CONFLICT(operation,object_id,version,processor_version) DO UPDATE SET state='pending',token=memory_jobs.token+1,attempts=0,lease_until='',error='',completed_at='',available_at=excluded.available_at`); err != nil {
				return err
			}
		}
		result = want
		return nil
	})
	return result, err
}

func (s *Store) SaveMemoryVectors(ctx context.Context, job core.MemoryJob, vectors []core.MemoryVector, now time.Time) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		if err := validateMemoryLease(ctx, tx, job, now); err != nil {
			return err
		}
		index, err := loadMemoryIndex(ctx, tx)
		if err != nil {
			return err
		}
		for _, vector := range vectors {
			_, err := loadMemoryRecord(ctx, tx, vector.ID)
			if err != nil {
				return err
			}
			if job.Operation != "embed" || vector.ID != job.ObjectID || fmt.Sprint(vector.Revision) != job.Version {
				return core.ErrMemoryConflict
			}
			var visible bool
			if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM memory_revisions v WHERE v.record_id=? AND v.revision=? AND `+memoryRevisionVisibility+`)`, vector.ID, vector.Revision).Scan(&visible); err != nil {
				return err
			}
			if !visible {
				return core.ErrMemoryExcluded
			}
			if vector.Generation != index.Generation || len(vector.Values) != index.Dimensions {
				return core.ErrMemoryIndexMismatch
			}
			data, err := encodeMemoryVector(vector.Values)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO memory_embeddings(record_id,revision,generation,dimensions,vector) VALUES(?,?,?,?,?) ON CONFLICT(record_id,revision,generation) DO UPDATE SET dimensions=excluded.dimensions,vector=excluded.vector`, vector.ID, vector.Revision, vector.Generation, len(vector.Values), data); err != nil {
				return err
			}
			codes, scale := quantizeMemoryVector(data)
			if _, err := tx.ExecContext(ctx, `INSERT INTO memory_vector_sketches(generation,record_id,revision,dimensions,scale,codes) VALUES(?,?,?,?,?,?) ON CONFLICT(generation,record_id,revision) DO UPDATE SET dimensions=excluded.dimensions,scale=excluded.scale,codes=excluded.codes`, vector.Generation, vector.ID, vector.Revision, len(vector.Values), scale, codes); err != nil {
				return err
			}
		}
		return finishMemoryJob(ctx, tx, job, "", now)
	})
}

func encodeMemoryVector(values []float32) ([]byte, error) {
	var norm float64
	for _, v := range values {
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			return nil, errors.New("embedding contains non-finite values")
		}
		norm += float64(v) * float64(v)
	}
	if norm == 0 {
		return nil, errors.New("embedding has zero norm")
	}
	norm = math.Sqrt(norm)
	data := make([]byte, len(values)*4)
	for i, v := range values {
		binary.LittleEndian.PutUint32(data[i*4:], math.Float32bits(float32(float64(v)/norm)))
	}
	return data, nil
}

func quantizeMemoryVector(normalized []byte) ([]byte, float64) {
	var largest float64
	for i := 0; i < len(normalized); i += 4 {
		largest = max(largest, math.Abs(float64(math.Float32frombits(binary.LittleEndian.Uint32(normalized[i:])))))
	}
	scale := largest / 127
	codes := make([]byte, len(normalized)/4)
	for i := range codes {
		v := float64(math.Float32frombits(binary.LittleEndian.Uint32(normalized[4*i:])))
		codes[i] = byte(int8(math.Round(v / scale)))
	}
	return codes, scale
}

const memoryVectorPageSQL = `SELECT e.record_id,e.revision,e.dimensions,e.scale,e.codes FROM memory_vector_sketches e JOIN memory_records r ON r.id=e.record_id AND r.revision=e.revision WHERE e.generation=? AND e.record_id>? AND r.excluded=0 ORDER BY e.record_id LIMIT ?`

const memoryHistoricalVectorPageSQL = `SELECT e.record_id,e.revision,e.dimensions,e.scale,e.codes FROM memory_vector_sketches e JOIN memory_records r ON r.id=e.record_id JOIN memory_revisions v ON v.record_id=e.record_id AND v.revision=e.revision WHERE e.generation=? AND e.record_id>? AND r.excluded=0 AND e.revision=(SELECT revision FROM memory_revisions WHERE record_id=e.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND ` + memoryRevisionVisibility + ` ORDER BY e.record_id LIMIT ?`

func (s *Store) MemoryVectorSketches(ctx context.Context, generation int64, after string, limit int, knownAt time.Time) ([]core.MemoryVectorSketch, error) {
	if limit <= 0 || limit > 512 {
		return nil, errors.New("vector batch limit must be between 1 and 512")
	}
	query, args := memoryVectorPageSQL, []any{generation, after, limit}
	if !knownAt.IsZero() {
		query, args = memoryHistoricalVectorPageSQL, []any{generation, after, formatTimestamp(knownAt), limit}
	}
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]core.MemoryVectorSketch, 0, limit)
	err = scanRows(rows, func(scan func(...any) error) error {
		var v core.MemoryVectorSketch
		var dims int
		var data sql.RawBytes
		if err := scan(&v.ID, &v.Revision, &dims, &v.Scale, &data); err != nil {
			return err
		}
		if len(data) != dims || v.Scale <= 0 || math.IsNaN(v.Scale) || math.IsInf(v.Scale, 0) {
			return fmt.Errorf("memory vector sketch %s has invalid dimensions or scale", v.ID)
		}
		v.Codes = append([]byte(nil), data...)
		out = append(out, v)
		return nil
	})
	return out, err
}

func (s *Store) MemoryVectorsByIDs(ctx context.Context, generation int64, ids []string, knownAt time.Time) ([]core.MemoryVector, error) {
	if len(ids) > 2048 {
		return nil, errors.New("exact vector candidate limit is 2048")
	}
	data, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	query := `SELECT e.record_id,e.revision,e.dimensions,e.vector FROM memory_embeddings e JOIN memory_records r ON r.id=e.record_id AND r.revision=e.revision WHERE e.generation=? AND e.record_id IN (SELECT value FROM json_each(?)) AND r.excluded=0`
	args := []any{generation, string(data)}
	if !knownAt.IsZero() {
		query = `SELECT e.record_id,e.revision,e.dimensions,e.vector FROM memory_embeddings e JOIN memory_records r ON r.id=e.record_id JOIN memory_revisions v ON v.record_id=e.record_id AND v.revision=e.revision WHERE e.generation=? AND e.record_id IN (SELECT value FROM json_each(?)) AND r.excluded=0 AND e.revision=(SELECT revision FROM memory_revisions WHERE record_id=e.record_id AND at<=? ORDER BY at DESC,revision DESC LIMIT 1) AND ` + memoryRevisionVisibility
		args = append(args, formatTimestamp(knownAt))
	}
	rows, err := s.read.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	out := make([]core.MemoryVector, 0, len(ids))
	err = scanRows(rows, func(scan func(...any) error) error {
		v := core.MemoryVector{Generation: generation}
		var dims int
		var raw sql.RawBytes
		if err := scan(&v.ID, &v.Revision, &dims, &raw); err != nil {
			return err
		}
		if len(raw) != dims*4 {
			return fmt.Errorf("memory vector %s has invalid dimensions", v.ID)
		}
		v.Values = make([]float32, dims)
		for i := range v.Values {
			v.Values[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
		out = append(out, v)
		return nil
	})
	return out, err
}

func (s *Store) CompleteMemoryIndex(ctx context.Context, generation int64) error {
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		index, err := loadMemoryIndex(ctx, tx)
		if err != nil {
			return err
		}
		if index.Generation != generation {
			return core.ErrMemoryIndexMismatch
		}
		var missing int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_revisions v JOIN memory_records r ON r.id=v.record_id WHERE r.excluded=0 AND `+memoryRevisionVisibility+` AND NOT EXISTS(SELECT 1 FROM memory_embeddings e JOIN memory_vector_sketches c ON c.record_id=e.record_id AND c.generation=e.generation AND c.revision=e.revision WHERE e.record_id=v.record_id AND e.revision=v.revision AND e.generation=?)`, generation).Scan(&missing); err != nil {
			return err
		}
		if missing != 0 {
			return fmt.Errorf("memory index has %d records awaiting embeddings", missing)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE memory_index_state SET state='ready' WHERE id=1 AND generation=?`, generation); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM memory_embeddings WHERE generation<>?`, generation)
		return err
	})
}
