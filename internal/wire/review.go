package wire

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/ycvk/acorn/internal/config"
	"github.com/ycvk/acorn/internal/review"
)

// buildReviewer returns nil when review is off.
func buildReviewer(cfg *config.Config) (*review.Jev, error) {
	if !cfg.ReviewEnabled() {
		return nil, nil
	}
	jev, err := review.NewJev(review.DefaultBaseURL, cfg.Review.APIKey, cfg.Review.Model,
		&http.Client{Timeout: time.Duration(cfg.Review.TimeoutSeconds) * time.Second})
	if err != nil {
		return nil, fmt.Errorf("review: %w", err)
	}
	return jev, nil
}

// ReviewStatus is what acorn doctor reports about action review.
type ReviewStatus struct {
	State string `json:"state"`
	Model string `json:"model,omitempty"`
	Error string `json:"error,omitempty"`
}

// ReviewStatus checks the configured reviewer with a minimal request.
func (c *Container) ReviewStatus(ctx context.Context) ReviewStatus {
	jev, err := buildReviewer(c.cfg)
	if err != nil {
		return ReviewStatus{State: "error", Error: err.Error()}
	}
	if jev == nil {
		return ReviewStatus{State: "off"}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	model, err := jev.Check(ctx)
	if err != nil {
		return ReviewStatus{State: "error", Model: c.cfg.Review.Model, Error: err.Error()}
	}
	return ReviewStatus{State: "on", Model: model}
}
