package wire

import (
	"testing"
)

// TestQuotaUnlimitedByDefault verifies that a zero dailyQuota (the default)
// never blocks runs.
func TestQuotaUnlimitedByDefault(t *testing.T) {
	creator := &triggerRunCreator{dailyQuota: 0}
	for i := 0; i < 100; i++ {
		if !creator.allowQuota() {
			t.Fatalf("run %d: unlimited quota should always allow", i)
		}
		creator.incQuota()
	}
}

// TestQuotaBlocksAfterLimit verifies that once the daily quota is reached,
// subsequent fires are blocked.
func TestQuotaBlocksAfterLimit(t *testing.T) {
	creator := &triggerRunCreator{dailyQuota: 3}
	for i := 0; i < 3; i++ {
		if !creator.allowQuota() {
			t.Fatalf("run %d: should be within quota", i)
		}
		creator.incQuota()
	}
	// 4th run should be blocked.
	if creator.allowQuota() {
		t.Fatal("run 4: should be blocked by daily quota")
	}
}

// TestQuotaResetsOnNewDay verifies that the quota counter resets when the
// UTC date changes. We simulate this by manually setting quotaDay to a past
// date and quotaUsed to the limit.
func TestQuotaResetsOnNewDay(t *testing.T) {
	creator := &triggerRunCreator{
		dailyQuota: 2,
		quotaDay:   "2020-01-01", // past date
		quotaUsed:  2,            // at limit
	}
	// allowQuota should detect the day changed and reset.
	if !creator.allowQuota() {
		t.Fatal("quota should reset on new day")
	}
	if creator.quotaUsed != 0 {
		t.Fatalf("quotaUsed should be 0 after reset, got %d", creator.quotaUsed)
	}
	// Now we should be able to run again.
	creator.incQuota()
	if creator.quotaUsed != 1 {
		t.Fatalf("quotaUsed should be 1 after one run, got %d", creator.quotaUsed)
	}
}
