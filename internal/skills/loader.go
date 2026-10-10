package skills

import (
	"errors"
)

var ErrNotFound = errors.New("skill not found")

// Loader reads skill packages from one directory; each package is a
// subdirectory holding SKILL.md.
type Loader struct {
	dir string
}

func NewLoader(dir string) *Loader {
	return &Loader{dir: dir}
}
