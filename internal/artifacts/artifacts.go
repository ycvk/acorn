package artifacts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Kind string

const (
	KindText       Kind = "text"
	KindMarkdown   Kind = "markdown"
	KindJSON       Kind = "json"
	KindDiff       Kind = "diff"
	KindLog        Kind = "log"
	KindTestReport Kind = "test_report"
	KindBinary     Kind = "binary"
)

var ErrArtifactNotFound = errors.New("artifact not found")

type Record struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                Kind
	Title               string
	MIMEType            string
	RelativePath        string
	SizeBytes           int64
	SHA256              string
	CreatedAt           time.Time
}

type WriteRequest struct {
	ArtifactID          string
	RunID               string
	SessionID           string
	SourceToolResultRef string
	Kind                Kind
	Title               string
	MIMEType            string
	Content             []byte
	CreatedAt           time.Time
}

type ReadRangeRequest struct {
	ArtifactID string
	Offset     int64
	Limit      int64
}

type ReadRangeResult struct {
	Record  Record
	Offset  int64
	Content []byte
	EOF     bool
}

type Store interface {
	SaveArtifact(context.Context, Record) (Record, error)
	LoadArtifact(context.Context, string) (Record, error)
	ListArtifactsByRun(context.Context, string) ([]Record, error)
	ListArtifactsBySession(context.Context, string) ([]Record, error)
}

type Service struct {
	rootDir string
	store   Store
}

func NewService(rootDir string, store Store) (*Service, error) {
	rootDir = strings.TrimSpace(rootDir)
	if rootDir == "" {
		return nil, fmt.Errorf("artifact root dir is required")
	}
	if store == nil {
		return nil, fmt.Errorf("artifact store is required")
	}
	cleanRoot, err := filepath.Abs(rootDir)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact root dir: %w", err)
	}
	if err := os.MkdirAll(cleanRoot, 0o755); err != nil {
		return nil, fmt.Errorf("create artifact root dir: %w", err)
	}
	return &Service{rootDir: cleanRoot, store: store}, nil
}

func (s *Service) Write(ctx context.Context, req WriteRequest) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	req, err := NormalizeWriteRequest(req)
	if err != nil {
		return Record{}, err
	}
	if req.ArtifactID == "" {
		req.ArtifactID, err = generateArtifactID()
		if err != nil {
			return Record{}, err
		}
	}
	sum := sha256.Sum256(req.Content)
	record := Record{
		ArtifactID:          req.ArtifactID,
		RunID:               req.RunID,
		SessionID:           req.SessionID,
		SourceToolResultRef: req.SourceToolResultRef,
		Kind:                req.Kind,
		Title:               req.Title,
		MIMEType:            req.MIMEType,
		RelativePath:        relativePathFor(req.RunID, req.ArtifactID),
		SizeBytes:           int64(len(req.Content)),
		SHA256:              hex.EncodeToString(sum[:]),
		CreatedAt:           req.CreatedAt,
	}
	record, err = NormalizeRecord(record)
	if err != nil {
		return Record{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return Record{}, err
	}
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return Record{}, fmt.Errorf("create artifact content dir: %w", err)
	}
	tmpPath := artifactPath + ".tmp-" + record.ArtifactID
	if err := os.WriteFile(tmpPath, req.Content, 0o600); err != nil {
		return Record{}, fmt.Errorf("write artifact content temp file: %w", err)
	}
	if err := os.Rename(tmpPath, artifactPath); err != nil {
		_ = os.Remove(tmpPath)
		return Record{}, fmt.Errorf("commit artifact content file: %w", err)
	}
	saved, err := s.store.SaveArtifact(ctx, record)
	if err != nil {
		removeErr := os.Remove(artifactPath)
		if removeErr != nil {
			return Record{}, errors.Join(fmt.Errorf("save artifact metadata: %w", err), fmt.Errorf("remove untracked artifact content: %w", removeErr))
		}
		return Record{}, fmt.Errorf("save artifact metadata: %w", err)
	}
	return saved, nil
}

func (s *Service) ReadRange(ctx context.Context, req ReadRangeRequest) (ReadRangeResult, error) {
	if s == nil {
		return ReadRangeResult{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return ReadRangeResult{}, err
	}
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	if req.ArtifactID == "" {
		return ReadRangeResult{}, fmt.Errorf("artifact_id is required")
	}
	if req.Offset < 0 {
		return ReadRangeResult{}, fmt.Errorf("artifact range offset must be >= 0")
	}
	if req.Limit <= 0 {
		return ReadRangeResult{}, fmt.Errorf("artifact range limit must be > 0")
	}
	record, err := s.store.LoadArtifact(ctx, req.ArtifactID)
	if err != nil {
		return ReadRangeResult{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return ReadRangeResult{}, err
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		return ReadRangeResult{}, fmt.Errorf("stat artifact content %s: %w", record.ArtifactID, err)
	}
	if info.Size() != record.SizeBytes {
		return ReadRangeResult{}, fmt.Errorf("artifact content size mismatch for %s: got %d want %d", record.ArtifactID, info.Size(), record.SizeBytes)
	}
	if req.Offset > record.SizeBytes {
		return ReadRangeResult{}, fmt.Errorf("artifact range offset %d exceeds size %d", req.Offset, record.SizeBytes)
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return ReadRangeResult{}, fmt.Errorf("open artifact content %s: %w", record.ArtifactID, err)
	}
	defer file.Close()
	if _, err := file.Seek(req.Offset, io.SeekStart); err != nil {
		return ReadRangeResult{}, fmt.Errorf("seek artifact content %s: %w", record.ArtifactID, err)
	}
	content, err := io.ReadAll(io.LimitReader(file, req.Limit))
	if err != nil {
		return ReadRangeResult{}, fmt.Errorf("read artifact content %s: %w", record.ArtifactID, err)
	}
	return ReadRangeResult{
		Record:  record,
		Offset:  req.Offset,
		Content: content,
		EOF:     req.Offset+int64(len(content)) >= record.SizeBytes,
	}, nil
}

func (s *Service) Verify(ctx context.Context, artifactID string) (Record, error) {
	if s == nil {
		return Record{}, fmt.Errorf("artifact service is nil")
	}
	if err := ctx.Err(); err != nil {
		return Record{}, err
	}
	artifactID = strings.TrimSpace(artifactID)
	if artifactID == "" {
		return Record{}, fmt.Errorf("artifact_id is required")
	}
	record, err := s.store.LoadArtifact(ctx, artifactID)
	if err != nil {
		return Record{}, err
	}
	artifactPath, err := s.pathFor(record.RelativePath)
	if err != nil {
		return Record{}, err
	}
	file, err := os.Open(artifactPath)
	if err != nil {
		return Record{}, fmt.Errorf("open artifact content %s: %w", record.ArtifactID, err)
	}
	defer file.Close()
	hash := sha256.New()
	size, err := io.Copy(hash, file)
	if err != nil {
		return Record{}, fmt.Errorf("hash artifact content %s: %w", record.ArtifactID, err)
	}
	if size != record.SizeBytes {
		return Record{}, fmt.Errorf("artifact content size mismatch for %s: got %d want %d", record.ArtifactID, size, record.SizeBytes)
	}
	gotHash := hex.EncodeToString(hash.Sum(nil))
	if gotHash != record.SHA256 {
		return Record{}, fmt.Errorf("artifact content sha256 mismatch for %s: got %s want %s", record.ArtifactID, gotHash, record.SHA256)
	}
	return record, nil
}

func (s *Service) ListByRun(ctx context.Context, runID string) ([]Record, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("artifact run_id is required")
	}
	return s.store.ListArtifactsByRun(ctx, runID)
}

func (s *Service) ListBySession(ctx context.Context, sessionID string) ([]Record, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("artifact session_id is required")
	}
	return s.store.ListArtifactsBySession(ctx, sessionID)
}

func NormalizeWriteRequest(req WriteRequest) (WriteRequest, error) {
	req.ArtifactID = strings.TrimSpace(req.ArtifactID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.SessionID = strings.TrimSpace(req.SessionID)
	req.SourceToolResultRef = strings.TrimSpace(req.SourceToolResultRef)
	req.Kind = Kind(strings.TrimSpace(string(req.Kind)))
	req.Title = strings.TrimSpace(req.Title)
	req.MIMEType = strings.TrimSpace(req.MIMEType)
	if req.CreatedAt.IsZero() {
		req.CreatedAt = time.Now().UTC()
	} else {
		req.CreatedAt = req.CreatedAt.UTC()
	}
	if req.ArtifactID != "" {
		if err := validateOpaqueID("artifact_id", req.ArtifactID); err != nil {
			return WriteRequest{}, err
		}
	}
	if req.RunID == "" {
		return WriteRequest{}, fmt.Errorf("artifact run_id is required")
	}
	if err := validateKind(req.Kind); err != nil {
		return WriteRequest{}, err
	}
	return req, nil
}

func NormalizeRecord(record Record) (Record, error) {
	record.ArtifactID = strings.TrimSpace(record.ArtifactID)
	record.RunID = strings.TrimSpace(record.RunID)
	record.SessionID = strings.TrimSpace(record.SessionID)
	record.SourceToolResultRef = strings.TrimSpace(record.SourceToolResultRef)
	record.Kind = Kind(strings.TrimSpace(string(record.Kind)))
	record.Title = strings.TrimSpace(record.Title)
	record.MIMEType = strings.TrimSpace(record.MIMEType)
	record.RelativePath = filepath.ToSlash(strings.TrimSpace(record.RelativePath))
	record.SHA256 = strings.ToLower(strings.TrimSpace(record.SHA256))
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now().UTC()
	} else {
		record.CreatedAt = record.CreatedAt.UTC()
	}
	if err := validateOpaqueID("artifact_id", record.ArtifactID); err != nil {
		return Record{}, err
	}
	if record.RunID == "" {
		return Record{}, fmt.Errorf("artifact run_id is required")
	}
	if err := validateKind(record.Kind); err != nil {
		return Record{}, err
	}
	if record.RelativePath == "" {
		return Record{}, fmt.Errorf("artifact relative_path is required")
	}
	if err := validateRelativePath(record.RelativePath); err != nil {
		return Record{}, err
	}
	if record.SizeBytes < 0 {
		return Record{}, fmt.Errorf("artifact size_bytes must be >= 0")
	}
	if len(record.SHA256) != sha256.Size*2 {
		return Record{}, fmt.Errorf("artifact sha256 must be %d hex characters", sha256.Size*2)
	}
	if _, err := hex.DecodeString(record.SHA256); err != nil {
		return Record{}, fmt.Errorf("artifact sha256 is invalid: %w", err)
	}
	return record, nil
}

func (s *Service) pathFor(relativePath string) (string, error) {
	if err := validateRelativePath(relativePath); err != nil {
		return "", err
	}
	fullPath := filepath.Join(s.rootDir, filepath.FromSlash(relativePath))
	cleanRoot := filepath.Clean(s.rootDir)
	cleanFull := filepath.Clean(fullPath)
	rel, err := filepath.Rel(cleanRoot, cleanFull)
	if err != nil {
		return "", fmt.Errorf("resolve artifact relative path: %w", err)
	}
	if rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || rel == ".." {
		return "", fmt.Errorf("artifact path escapes root: %s", relativePath)
	}
	return cleanFull, nil
}

func validateKind(kind Kind) error {
	switch kind {
	case KindText, KindMarkdown, KindJSON, KindDiff, KindLog, KindTestReport, KindBinary:
		return nil
	default:
		return fmt.Errorf("artifact kind %q is invalid", kind)
	}
}

func validateOpaqueID(field string, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}
	if value == "." || value == ".." || strings.ContainsAny(value, `/\`) {
		return fmt.Errorf("%s must be an opaque identifier", field)
	}
	return nil
}

func validateRelativePath(value string) error {
	if filepath.IsAbs(value) {
		return fmt.Errorf("artifact relative_path must not be absolute")
	}
	clean := filepath.Clean(filepath.FromSlash(value))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("artifact relative_path must stay under artifact root")
	}
	return nil
}

func relativePathFor(runID string, artifactID string) string {
	hash := sha256.Sum256([]byte(runID))
	return filepath.ToSlash(filepath.Join("runs", hex.EncodeToString(hash[:8]), artifactID))
}

func generateArtifactID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate artifact id: %w", err)
	}
	return "artifact_" + hex.EncodeToString(raw[:]), nil
}
