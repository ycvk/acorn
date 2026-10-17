package wake

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// A backup or a memory job older than these is a problem worth a line in the
// morning briefing.
const (
	backupStaleAfter = 36 * time.Hour
	memoryStaleAfter = 24 * time.Hour
)

// briefingHealth lists the problems the owner should act on, for the morning
// briefing; it is empty when everything is healthy.
func (s *Scheduler) briefingHealth(ctx context.Context, now time.Time) (string, error) {
	var problems []string
	if s.cfg.Backups != nil {
		latest, err := s.cfg.Backups()
		switch {
		case err != nil:
			problems = append(problems, "Backups: the backup directory cannot be read: "+err.Error())
		case latest.IsZero():
			problems = append(problems, "Backups: no snapshot has been taken yet; check acorn-backup.timer.")
		case now.Sub(latest) > backupStaleAfter:
			problems = append(problems, fmt.Sprintf("Backups: the newest snapshot is from %s, %d hours ago; check acorn-backup.timer.", latest.In(s.cfg.Location).Format("2006-01-02 15:04"), int(now.Sub(latest).Hours())))
		}
	}
	status, err := s.cfg.Memory.MemoryProcessingStatus(ctx)
	if err != nil {
		return "", err
	}
	if status.Failed > 0 {
		problems = append(problems, fmt.Sprintf("Memory processing: %d jobs failed after all retries; last error: %s", status.Failed, previewText(status.LastError, 300)))
	}
	if status.Pending > 0 && !status.Oldest.IsZero() && now.Sub(status.Oldest) > memoryStaleAfter {
		line := fmt.Sprintf("Memory processing: %d jobs are waiting, the oldest since %s", status.Pending, status.Oldest.In(s.cfg.Location).Format("2006-01-02 15:04"))
		if status.DailyTokenLimit > 0 {
			line += fmt.Sprintf("; %d of the %d daily memory tokens are used", status.TokensToday, status.DailyTokenLimit)
		}
		problems = append(problems, line+".")
	}
	triggers, err := s.cfg.Triggers.ListTriggers(ctx)
	if err != nil {
		return "", err
	}
	problems = append(problems, failingTriggers(triggers)...)
	if len(problems) == 0 {
		return "", nil
	}
	return "System health (problems for the owner to act on):\n- " + strings.Join(problems, "\n- ") + "\n", nil
}
