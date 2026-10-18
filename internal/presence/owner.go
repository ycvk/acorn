package presence

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ycvk/acorn/internal/core"
)

// MaxOwnerNameRunes bounds the owner's name, which enters every run's
// instruction and every extraction.
const MaxOwnerNameRunes = 40

// SaveOwnerName saves the trimmed name as what Acorn calls the owner; an
// empty name leaves the owner unnamed.
func SaveOwnerName(ctx context.Context, store core.OwnerStore, name string, at time.Time) (core.OwnerName, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n > MaxOwnerNameRunes {
		return core.OwnerName{}, fmt.Errorf("%w: the name is %d characters, over the %d character limit", core.ErrOwnerNameInvalid, n, MaxOwnerNameRunes)
	}
	if strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return core.OwnerName{}, fmt.Errorf("%w: the name holds a line break or another control character", core.ErrOwnerNameInvalid)
	}
	return store.SetOwnerName(ctx, name, at)
}
