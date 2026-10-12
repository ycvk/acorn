package tools

import (
	"context"

	"github.com/ycvk/acorn/internal/webaccess"
)

type WebFetchService interface {
	Fetch(ctx context.Context, req webaccess.FetchRequest) (webaccess.FetchResult, error)
}

// WebSearchService is the subset of web search operations required by tool builders.
type WebSearchService interface {
	Search(ctx context.Context, req webaccess.SearchRequest) (webaccess.SearchResult, error)
}
