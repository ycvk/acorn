package wake

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ycvk/acorn/internal/core"
)

const (
	// briefingLead is how long before the owner's briefing time the briefing
	// starts, so the pieces about its focus are written by that time.
	briefingLead = 5 * time.Minute
	// pieceWait is how long after a briefing finished its push waits for the
	// pieces about its focus.
	pieceWait = 5 * time.Minute
	// pieceWindow is how long after a briefing finished its pieces are still
	// started; an older briefing goes without.
	pieceWindow = 12 * time.Hour
)

// advanceBriefings takes each recorded briefing through what is left of it:
// it starts the pieces about the briefing's focus, once, and pushes the
// briefing, once, when those pieces have ended or pieceWait has passed, and
// not before the owner's briefing time.
func (s *Scheduler) advanceBriefings(ctx context.Context, now time.Time, settings core.Settings) error {
	open, err := s.cfg.News.OpenBriefings(ctx, now.Add(-core.NewsRetention))
	if err != nil {
		return err
	}
	var errs []error
	for _, briefing := range open {
		if !briefing.PiecesStarted {
			if err := s.startPieces(ctx, briefing, now); err != nil {
				errs = append(errs, fmt.Errorf("pieces of briefing %s: %w", briefing.RunID, err))
			}
		}
		if !briefing.Pushed {
			ready, err := s.claimPush(ctx, briefing, now, settings)
			if err != nil {
				errs = append(errs, fmt.Errorf("push briefing %s: %w", briefing.RunID, err))
			} else if ready {
				if err := s.pushBriefing(ctx, briefing.RoutineOutput); err != nil {
					errs = append(errs, err)
				}
			}
		}
	}
	return errors.Join(errs...)
}

// startPieces starts the run that writes the piece about each focus story of
// a briefing. The briefing is claimed first, so a story whose run could not
// start goes without a piece: a story pruned since and a conversation the
// owner is in are logged, anything else is returned.
func (s *Scheduler) startPieces(ctx context.Context, briefing core.OpenBriefing, now time.Time) error {
	claimed, err := s.cfg.News.ClaimBriefingPieces(ctx, briefing.RunID, now)
	if err != nil || !claimed || now.Sub(briefing.FinishedAt) > pieceWindow {
		return err
	}
	stories, err := s.cfg.News.BriefingStories(ctx, briefing.RunID)
	if err != nil {
		return err
	}
	var errs []error
	for _, story := range stories {
		if !story.Focus {
			continue
		}
		_, err := s.cfg.Runs.StartStoryPiece(ctx, story.StoryID, briefing.Slot)
		switch {
		case errors.Is(err, core.ErrNewsStoryNotFound) || errors.Is(err, core.ErrThreadBusy):
			slog.Warn("briefing story goes without a piece", "run_id", briefing.RunID, "story_id", story.StoryID, "error", err)
		case err != nil:
			errs = append(errs, fmt.Errorf("story %d: %w", story.StoryID, err))
		}
	}
	return errors.Join(errs...)
}

// claimPush reports whether this tick is to push a briefing: the owner's
// briefing time has come, none of its pieces is still being written or
// pieceWait has passed since it finished, and this call claimed the push.
func (s *Scheduler) claimPush(ctx context.Context, briefing core.OpenBriefing, now time.Time, settings core.Settings) (bool, error) {
	at, err := briefingTime(briefing.Slot, settings)
	if err != nil {
		return false, err
	}
	if now.Before(at) {
		return false, nil
	}
	if now.Before(briefing.FinishedAt.Add(pieceWait)) {
		stories, err := s.cfg.News.BriefingStories(ctx, briefing.RunID)
		if err != nil {
			return false, err
		}
		for _, story := range stories {
			if story.Piece != nil && story.Piece.Status == core.BriefingPieceWriting {
				return false, nil
			}
		}
	}
	return s.cfg.News.ClaimBriefingPush(ctx, briefing.RunID, now)
}

// briefingTime is the owner's briefing time on the day of a briefing's slot.
func briefingTime(slot string, settings core.Settings) (time.Time, error) {
	day, err := time.ParseInLocation("2006-01-02", slot, settings.Location)
	if err != nil {
		return time.Time{}, fmt.Errorf("briefing slot %q: %w", slot, err)
	}
	at := settings.Briefing.At
	return time.Date(day.Year(), day.Month(), day.Day(), int(at/time.Hour), int(at%time.Hour/time.Minute), 0, 0, settings.Location), nil
}
