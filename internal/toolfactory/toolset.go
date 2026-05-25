package toolfactory

import (
	"errors"
	"io"

	einotool "github.com/cloudwego/eino/components/tool"

	"github.com/ycvk/acorn/internal/tooling"
)

func NewToolset(catalog *tooling.Catalog, profile tooling.ToolProfile, closers ...io.Closer) *Toolset {
	c := make([]closer, 0, len(closers))
	for _, cl := range closers {
		if cl != nil {
			c = append(c, cl)
		}
	}
	return &Toolset{catalog: catalog, profile: profile, closers: c}
}

// Toolset is a built collection of tools for a run or serve profile.
type Toolset struct {
	catalog *tooling.Catalog
	profile tooling.ToolProfile
	closers []closer
}

type closer interface {
	Close() error
}

func (t Toolset) All() []einotool.BaseTool {
	if t.catalog == nil {
		return nil
	}
	return t.catalog.ToolsForProfile(t.profile)
}

func (t Toolset) Catalog() *tooling.Catalog {
	return t.catalog
}

func (t *Toolset) Close() error {
	if t == nil {
		return nil
	}
	var errs []error
	for i := len(t.closers) - 1; i >= 0; i-- {
		if t.closers[i] == nil {
			continue
		}
		if err := t.closers[i].Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}
