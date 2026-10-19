package workstation

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"testing/synctest"
	"time"
)

// played replays the events sent by feed and lists each performed event with
// when it was performed, counted from the down's arrival.
func played(t *testing.T, feed func(chan<- dragEvent)) ([]string, error) {
	t.Helper()
	var out []string
	var err error
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		events := make(chan dragEvent, maxDragBacklog)
		done := make(chan error, 1)
		go func() {
			done <- replay(t.Context(), start.Add(dragDelay), events, func(ev dragEvent) error {
				out = append(out, fmt.Sprintf("%s %v @%s", ev.kind, ev.x, time.Since(start)))
				return nil
			})
		}()
		feed(events)
		err = <-done
	})
	return out, err
}

// batch sends moves at x 1, 2, ... with the times given.
func batch(events chan<- dragEvent, times ...int64) {
	for _, t := range times {
		events <- dragEvent{kind: "move", x: float64(t), t: t}
	}
}

func TestReplayPlaysEachEventAtItsTimeAfterTheDelay(t *testing.T) {
	got, err := played(t, func(events chan<- dragEvent) {
		events <- dragEvent{kind: "down"}
		// Batches arrive every 70 ms or so, each with the points since the last.
		time.Sleep(60 * time.Millisecond)
		batch(events, 8, 16, 24, 33, 41, 49)
		time.Sleep(75 * time.Millisecond)
		batch(events, 58, 66, 74, 83, 91, 99, 108, 116, 124)
		time.Sleep(70 * time.Millisecond)
		batch(events, 133)
		events <- dragEvent{kind: "up", x: 133, t: 140}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"down 0 @150ms"}
	for _, ms := range []int64{8, 16, 24, 33, 41, 49, 58, 66, 74, 83, 91, 99, 108, 116, 124, 133} {
		want = append(want, fmt.Sprintf("move %d @%dms", ms, 150+ms))
	}
	want = append(want, "up 133 @290ms")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("played\n%v\nwant\n%v", got, want)
	}
}

func TestReplayKeepsTheSpacingAfterALateBatch(t *testing.T) {
	got, err := played(t, func(events chan<- dragEvent) {
		events <- dragEvent{kind: "down"}
		time.Sleep(60 * time.Millisecond)
		batch(events, 20, 40)
		// The next batch is held up on the network until its first point is
		// 150 ms overdue.
		time.Sleep(340 * time.Millisecond)
		batch(events, 100, 120, 140)
		events <- dragEvent{kind: "up", x: 140, t: 160}
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"down 0 @150ms", "move 20 @170ms", "move 40 @190ms", "move 100 @400ms", "move 120 @420ms", "move 140 @440ms", "up 140 @460ms"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("played %v, want %v", got, want)
	}
}

func TestReplayEndsADragWithoutItsUp(t *testing.T) {
	got, err := played(t, func(events chan<- dragEvent) {
		events <- dragEvent{kind: "down"}
		batch(events, 10)
	})
	if err == nil || !strings.Contains(err.Error(), "no move or up for") {
		t.Fatalf("err = %v", err)
	}
	if want := []string{"down 0 @150ms", "move 10 @160ms"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("played %v", got)
	}
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		events := make(chan dragEvent, 1)
		events <- dragEvent{kind: "down"}
		done := make(chan error, 1)
		go func() { done <- replay(ctx, time.Now().Add(dragDelay), events, func(dragEvent) error { return nil }) }()
		cancel()
		if err := <-done; err != context.Canceled {
			t.Fatalf("cancelled err = %v", err)
		}
	})
}
