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
// briefing; it is empty when everything is healthy. Work Acorn gave up on
// counts only while that kind of work has not succeeded since.
func (s *Scheduler) briefingHealth(ctx context.Context, now time.Time, loc *time.Location) (string, error) {
	var problems []string
	if s.cfg.Backups != nil {
		latest, err := s.cfg.Backups()
		switch {
		case err != nil:
			problems = append(problems, "Backups: the backup directory cannot be read: "+err.Error())
		case latest.IsZero():
			problems = append(problems, "Backups: no snapshot has been taken yet; check acorn-backup.timer.")
		case now.Sub(latest) > backupStaleAfter:
			problems = append(problems, fmt.Sprintf("Backups: the newest snapshot is from %s, %d hours ago; check acorn-backup.timer.", latest.In(loc).Format("2006-01-02 15:04"), int(now.Sub(latest).Hours())))
		}
	}
	status, err := s.cfg.Memory.MemoryProcessingStatus(ctx)
	if err != nil {
		return "", err
	}
	if status.Pending > 0 && !status.Oldest.IsZero() && now.Sub(status.Oldest) > memoryStaleAfter {
		line := fmt.Sprintf("Memory processing: %d jobs are waiting, the oldest since %s", status.Pending, status.Oldest.In(loc).Format("2006-01-02 15:04"))
		if status.DailyTokenLimit > 0 {
			line += fmt.Sprintf("; %d of the %d daily memory tokens are used", status.TokensToday, status.DailyTokenLimit)
		}
		problems = append(problems, line+".")
	}
	failing, err := s.cfg.Memory.FailingMemoryWork(ctx)
	if err != nil {
		return "", err
	}
	for _, work := range failing {
		problems = append(problems, fmt.Sprintf("Memory processing: %d %s jobs failed after all retries since %s, and none has succeeded since; last error: %s; check `acorn doctor` and `journalctl -u acorn`.",
			work.Failed, work.Kind, work.Since.In(loc).Format("2006-01-02 15:04"), previewText(work.LastError, 300)))
	}
	triggers, err := s.cfg.Triggers.ListTriggers(ctx)
	if err != nil {
		return "", err
	}
	problems = append(problems, failingTriggers(triggers)...)
	news, err := s.newsHealth(ctx, loc)
	if err != nil {
		return "", err
	}
	problems = append(problems, news...)
	if len(problems) == 0 {
		return "", nil
	}
	return "System health (problems for the owner to act on):\n- " + strings.Join(problems, "\n- ") + "\n", nil
}
