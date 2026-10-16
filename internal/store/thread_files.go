package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

// threadFilesTable is shared with the migration that adds it. A file's
// content is kept as files/<file_id> in the storage directory.
const threadFilesTable = `CREATE TABLE IF NOT EXISTS thread_files (
    file_id TEXT PRIMARY KEY,
    session_id TEXT NOT NULL,
    message_id INTEGER NOT NULL DEFAULT 0,
    run_id TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL CHECK (origin IN ('owner','agent')),
    name TEXT NOT NULL,
    mime_type TEXT NOT NULL,
    size_bytes INTEGER NOT NULL,
    sha256 TEXT NOT NULL,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_thread_files_session ON thread_files(session_id, created_at);`

const threadFilesDir = "files"

var threadFileID = regexp.MustCompile(`^file_[0-9a-f]{32}$`)

const threadFileColumns = `file_id, session_id, message_id, run_id, origin, name, mime_type, size_bytes, sha256, created_at`

// SaveThreadFile stores a file in a thread. The content is written beside
// its final place and moved there before the row is added.
func (s *Store) SaveThreadFile(ctx context.Context, in core.ThreadFileWrite) (core.ThreadFile, error) {
	switch {
	case in.Origin != core.ThreadFileFromOwner && in.Origin != core.ThreadFileFromAgent:
		return core.ThreadFile{}, fmt.Errorf("thread file origin %q must be owner or agent", in.Origin)
	case in.Origin == core.ThreadFileFromAgent && strings.TrimSpace(in.RunID) == "":
		return core.ThreadFile{}, errors.New("an agent thread file needs its run_id")
	case in.Content == nil || in.MaxBytes < 1:
		return core.ThreadFile{}, errors.New("thread file needs content and a positive MaxBytes")
	}
	name, err := core.ThreadFileName(in.Name)
	if err != nil {
		return core.ThreadFile{}, err
	}
	var sessions int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sessions WHERE session_id = ?`, in.SessionID).Scan(&sessions); err != nil {
		return core.ThreadFile{}, fmt.Errorf("check thread %s: %w", in.SessionID, err)
	}
	if sessions == 0 {
		return core.ThreadFile{}, fmt.Errorf("%w: %s", core.ErrSessionNotFound, in.SessionID)
	}
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return core.ThreadFile{}, fmt.Errorf("thread file id: %w", err)
	}
	file := core.ThreadFile{ID: "file_" + hex.EncodeToString(raw[:]), SessionID: in.SessionID, RunID: strings.TrimSpace(in.RunID), Origin: in.Origin, Name: name, CreatedAt: time.Now().UTC()}
	final := filepath.Join(s.filesDir, file.ID)
	head, size, sum, err := writeThreadFile(s.filesDir, final, in.Content, in.MaxBytes)
	if err != nil {
		return core.ThreadFile{}, err
	}
	file.Size, file.SHA256, file.MIME = size, sum, core.ThreadFileMIME(name, head)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO thread_files (`+threadFileColumns+`) VALUES (?, ?, 0, ?, ?, ?, ?, ?, ?, ?)`,
		file.ID, file.SessionID, file.RunID, file.Origin, file.Name, file.MIME, file.Size, file.SHA256, formatTimestamp(file.CreatedAt)); err != nil {
		return core.ThreadFile{}, errors.Join(fmt.Errorf("save thread file: %w", err), os.Remove(final))
	}
	return file, nil
}

// writeThreadFile copies content into final through a temporary file and
// returns the first bytes, the size and the SHA-256 of what it wrote.
func writeThreadFile(dir, final string, content io.Reader, maxBytes int64) (head []byte, size int64, sum string, err error) {
	tmp, err := os.CreateTemp(dir, ".incoming-*")
	if err != nil {
		return nil, 0, "", fmt.Errorf("thread file: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	hash := sha256.New()
	first := &headWriter{max: 512}
	size, err = io.Copy(io.MultiWriter(tmp, hash, first), io.LimitReader(content, maxBytes+1))
	switch {
	case err != nil:
		return nil, 0, "", fmt.Errorf("thread file: %w", err)
	case size > maxBytes:
		return nil, 0, "", fmt.Errorf("%w: larger than %d bytes", core.ErrThreadFileTooLarge, maxBytes)
	case size == 0:
		return nil, 0, "", fmt.Errorf("%w: the file is empty", core.ErrInvalidThreadFile)
	}
	if err := tmp.Sync(); err != nil {
		return nil, 0, "", fmt.Errorf("thread file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, 0, "", fmt.Errorf("thread file: %w", err)
	}
	if err := os.Rename(tmp.Name(), final); err != nil {
		return nil, 0, "", fmt.Errorf("thread file: %w", err)
	}
	return first.data, size, hex.EncodeToString(hash.Sum(nil)), nil
}

// headWriter keeps the first max bytes written to it.
type headWriter struct {
	data []byte
	max  int
}

func (w *headWriter) Write(p []byte) (int, error) {
	if room := w.max - len(w.data); room > 0 {
		w.data = append(w.data, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// ThreadFiles lists a thread's files, oldest first.
func (s *Store) ThreadFiles(ctx context.Context, sessionID string) ([]core.ThreadFile, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+threadFileColumns+` FROM thread_files WHERE session_id = ? ORDER BY created_at, file_id`, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list thread files: %w", err)
	}
	files := []core.ThreadFile{}
	err = scanRows(rows, func(scan func(...any) error) error {
		file, err := scanThreadFile(scan)
		if err != nil {
			return err
		}
		files = append(files, file)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("list thread files: %w", err)
	}
	return files, nil
}

func scanThreadFile(scan func(...any) error) (core.ThreadFile, error) {
	var file core.ThreadFile
	var created string
	if err := scan(&file.ID, &file.SessionID, &file.MessageID, &file.RunID, &file.Origin, &file.Name, &file.MIME, &file.Size, &file.SHA256, &created); err != nil {
		return core.ThreadFile{}, err
	}
	at, err := parseTimestamp(fixedTimestampLayout, created, "thread_files.created_at")
	if err != nil {
		return core.ThreadFile{}, err
	}
	file.CreatedAt = at
	return file, nil
}

// OpenThreadFile opens a file of the thread sessionID for reading.
func (s *Store) OpenThreadFile(ctx context.Context, sessionID, fileID string) (core.ThreadFile, io.ReadCloser, error) {
	if !threadFileID.MatchString(fileID) {
		return core.ThreadFile{}, nil, fmt.Errorf("%w: %q", core.ErrThreadFileNotFound, fileID)
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+threadFileColumns+` FROM thread_files WHERE file_id = ? AND session_id = ?`, fileID, sessionID)
	file, err := scanThreadFile(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return core.ThreadFile{}, nil, fmt.Errorf("%w: %s in thread %s", core.ErrThreadFileNotFound, fileID, sessionID)
	}
	if err != nil {
		return core.ThreadFile{}, nil, fmt.Errorf("load thread file %s: %w", fileID, err)
	}
	content, err := os.Open(filepath.Join(s.filesDir, file.ID))
	if err != nil {
		return core.ThreadFile{}, nil, fmt.Errorf("open thread file %s: %w", fileID, err)
	}
	info, err := content.Stat()
	if err != nil || info.Size() != file.Size {
		_ = content.Close()
		return core.ThreadFile{}, nil, fmt.Errorf("thread file %s on disk does not match its record (stat: %v)", fileID, err)
	}
	return file, content, nil
}

// AttachThreadFiles sends unsent owner files of the thread with messageID.
func (s *Store) AttachThreadFiles(ctx context.Context, sessionID string, messageID int64, fileIDs []string) error {
	if messageID < 1 || len(fileIDs) == 0 {
		return errors.New("attaching thread files needs a message id and file ids")
	}
	placeholders, args := inClause(fileIDs)
	if len(args) != len(fileIDs) {
		return fmt.Errorf("%w: file ids repeat", core.ErrInvalidThreadFile)
	}
	return s.memoryTransaction(ctx, func(tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, `UPDATE thread_files SET message_id = ? WHERE session_id = ? AND origin = 'owner' AND message_id = 0 AND file_id IN (`+placeholders+`)`,
			append([]any{messageID, sessionID}, args...)...)
		if err != nil {
			return fmt.Errorf("attach thread files: %w", err)
		}
		attached, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("attach thread files: %w", err)
		}
		if attached != int64(len(fileIDs)) {
			return fmt.Errorf("%w: only %d of %d files are unsent owner files of thread %s", core.ErrInvalidThreadFile, attached, len(fileIDs), sessionID)
		}
		return nil
	})
}

// OwnerThreadFileIDs lists the owner files of every thread.
func (s *Store) OwnerThreadFileIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT file_id FROM thread_files WHERE origin = 'owner' ORDER BY file_id`)
	if err != nil {
		return nil, fmt.Errorf("list owner thread files: %w", err)
	}
	return memoryStringRows(rows)
}

// purgeThreadFiles deletes a thread's file rows and returns the ids whose
// content the caller removes after the transaction commits.
func purgeThreadFiles(ctx context.Context, tx *sql.Tx, sessionID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT file_id FROM thread_files WHERE session_id = ?`, sessionID)
	if err != nil {
		return nil, err
	}
	ids, err := memoryStringRows(rows)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM thread_files WHERE session_id = ?`, sessionID); err != nil {
		return nil, err
	}
	return ids, nil
}

// addThreadFiles (9 to 10) adds the files of conversations.
func addThreadFiles(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, threadFilesTable)
	return err
}
