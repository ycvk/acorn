package knowledge

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Git records knowledge changes as commits.
type Git interface {
	// Init makes dir a repository when it is not one and lets the owner push
	// into its checked-out branch (receive.denyCurrentBranch=updateInstead).
	Init(ctx context.Context, dir string) error
	// Commit stages exactly paths and commits them with message. It returns
	// "" when the paths have no changes.
	Commit(ctx context.Context, dir string, paths []string, message string) (string, error)
	Version(ctx context.Context) (string, error)
}

// ExecGit runs the system git binary. Commits use a fixed author so the
// server needs no git identity, and only the given paths are committed so
// other uncommitted edits in the directory are left alone.
type ExecGit struct {
	Binary string
}

// LookupGit finds git on PATH.
func LookupGit() (*ExecGit, error) {
	binary, err := exec.LookPath("git")
	if err != nil {
		return nil, errors.New("the knowledge base needs git, which was not found on PATH; install it (for example: apt install git)")
	}
	return &ExecGit{Binary: binary}, nil
}

func (g *ExecGit) Init(ctx context.Context, dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); errors.Is(err, os.ErrNotExist) {
		if _, err := g.run(ctx, dir, "init", "-q"); err != nil {
			return err
		}
	} else if err != nil {
		return fmt.Errorf("inspect %s: %w", dir, err)
	}
	_, err := g.run(ctx, dir, "config", "receive.denyCurrentBranch", "updateInstead")
	return err
}

func (g *ExecGit) Commit(ctx context.Context, dir string, paths []string, message string) (string, error) {
	if len(paths) == 0 {
		return "", errors.New("git commit: no paths")
	}
	if _, err := g.run(ctx, dir, append([]string{"add", "--"}, paths...)...); err != nil {
		return "", err
	}
	if _, err := g.run(ctx, dir, append([]string{"diff", "--cached", "--quiet", "--"}, paths...)...); err == nil {
		return "", nil
	} else if !isExitCode(err, 1) {
		return "", err
	}
	args := []string{
		"-c", "user.name=Acorn", "-c", "user.email=acorn@localhost", "-c", "commit.gpgsign=false",
		"commit", "-q", "--no-verify", "-m", message, "--",
	}
	if _, err := g.run(ctx, dir, append(args, paths...)...); err != nil {
		return "", err
	}
	sha, err := g.run(ctx, dir, "rev-parse", "HEAD")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(sha), nil
}

func (g *ExecGit) Version(ctx context.Context) (string, error) {
	out, err := g.run(ctx, "", "--version")
	return strings.TrimSpace(out), err
}

type gitError struct {
	args   []string
	stderr string
	err    error
}

func (e *gitError) Error() string {
	return fmt.Sprintf("git %s: %v: %s", strings.Join(e.args, " "), e.err, strings.TrimSpace(e.stderr))
}

func (e *gitError) Unwrap() error { return e.err }

func (g *ExecGit) run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, g.Binary, args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", &gitError{args: args, stderr: stderr.String(), err: err}
	}
	return stdout.String(), nil
}

func isExitCode(err error, code int) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && exitErr.ExitCode() == code
}
