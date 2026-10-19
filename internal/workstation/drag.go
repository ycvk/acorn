package workstation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// DragPoint is where the owner's finger was during a drag, in the stream's
// coordinates, T milliseconds after it went down.
type DragPoint struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	T int64   `json:"t"`
}

// dragDelay is how long after a drag's down arrives its events start to play
// on the page. Moves reach the workstation in batches over the owner's
// network; the delay lets a batch arrive before its points are due, so the
// page gets them at the spacing the finger made.
const dragDelay = 150 * time.Millisecond

// dragIdle ends a drag whose next move or up has not come for this long, as
// when the app lost its connection mid-drag.
const dragIdle = 30 * time.Second

// dragLead is how far a point's time may run ahead of the drag's time on the
// workstation. The app sends the down once it knows the touch is a drag, up to
// a long press after the finger went down, and the points it saw meanwhile
// follow.
const dragLead = 2 * time.Second

// maxDragBacklog bounds the events of a drag waiting for their time.
const maxDragBacklog = 4096

// ErrNoDrag is returned for a move or up without a drag in progress on its tab.
var ErrNoDrag = errors.New("no drag in progress on this tab")

// drag is one drag in progress: a press, moves with the button held and a
// release, all on one DevTools session with the page's visual viewport read
// once at the press.
type drag struct {
	tab      string
	viewport cssVisualViewport
	started  time.Time
	events   chan dragEvent
	cancel   context.CancelFunc
	done     chan struct{}
	// last is the time of the latest event queued and lifted whether the up
	// was; the View's mu guards both.
	last   int64
	lifted bool
	// err is why the drag ended, set before done closes.
	err error
}

// dragEvent is a press, a move or a release at a point on the page, t
// milliseconds after the finger went down.
type dragEvent struct {
	kind string
	x, y float64
	t    int64
}

func (d *drag) ended() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

// dragState is the drag in progress, or a channel that closes when one
// begins.
func (v *View) dragState() (*drag, <-chan struct{}) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.drag != nil && !v.drag.ended() {
		return v.drag, nil
	}
	return nil, v.dragged
}

// dragDown presses the button where the finger went down and starts playing
// the drag. A drag still in progress lost its up and is released first; one
// already lifted finishes first.
func (v *View) dragDown(ctx context.Context, in Input) error {
	if previous, _ := v.dragState(); previous != nil {
		v.mu.Lock()
		lifted := previous.lifted
		v.mu.Unlock()
		if !lifted {
			previous.cancel()
		}
		select {
		case <-previous.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	conn, err := v.attach(ctx, in.Tab)
	if err != nil {
		return err
	}
	if err := v.hold(ctx); err != nil {
		conn.close()
		return err
	}
	viewport, err := visualViewport(ctx, conn)
	if err != nil {
		v.unhold()
		conn.close()
		return err
	}
	playCtx, cancel := context.WithCancel(context.Background())
	d := &drag{tab: in.Tab, viewport: viewport, started: time.Now(), events: make(chan dragEvent, maxDragBacklog), cancel: cancel, done: make(chan struct{})}
	at := viewport.toPage(in)
	d.events <- dragEvent{kind: "down", x: at.X, y: at.Y}
	v.mu.Lock()
	v.drag = d
	close(v.dragged)
	v.dragged = make(chan struct{})
	v.mu.Unlock()
	go v.play(playCtx, d, conn, d.started.Add(dragDelay))
	return nil
}

// dragMove queues the finger's points for their time and returns without
// waiting for them.
func (v *View) dragMove(in Input) error {
	if len(in.Points) == 0 {
		return errors.New("move needs points")
	}
	events := make([]dragEvent, 0, len(in.Points))
	for _, p := range in.Points {
		events = append(events, dragEvent{kind: "move", x: p.X, y: p.Y, t: p.T})
	}
	_, err := v.queue(in.Tab, events)
	return err
}

// dragUp queues the release and waits until the drag has played, so a failure
// anywhere in it is the up's.
func (v *View) dragUp(ctx context.Context, in Input) error {
	d, err := v.queue(in.Tab, []dragEvent{{kind: "up", x: in.X, y: in.Y, t: in.T}})
	if err != nil {
		return err
	}
	select {
	case <-d.done:
		return d.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// queue maps events from the stream onto the page and hands them to the
// player of the tab's drag, after checking they continue it in time order.
func (v *View) queue(tab string, events []dragEvent) (*drag, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	d := v.drag
	if d == nil || d.tab != tab || d.lifted {
		return nil, ErrNoDrag
	}
	if d.ended() {
		return nil, fmt.Errorf("the drag has ended: %w", d.err)
	}
	ahead := time.Since(d.started) + dragLead
	last := d.last
	for _, ev := range events {
		if ev.t < last {
			return nil, fmt.Errorf("drag points must be in time order: t %d after %d", ev.t, last)
		}
		if time.Duration(ev.t)*time.Millisecond > ahead {
			return nil, fmt.Errorf("drag point t %d is ahead of the drag", ev.t)
		}
		last = ev.t
	}
	for _, ev := range events {
		at := d.viewport.toPage(Input{X: ev.x, Y: ev.y})
		ev.x, ev.y = at.X, at.Y
		select {
		case d.events <- ev:
		default:
			d.cancel()
			return nil, fmt.Errorf("the drag has more than %d events waiting", maxDragBacklog)
		}
	}
	d.last = last
	d.lifted = events[len(events)-1].kind == "up"
	return d, nil
}

// play performs a drag on conn and ends it. The browser answers an input
// once the page has handled it, which can take until the page's next frame,
// so each input goes at its own time and the answers are checked as they
// come. A drag that ends without its up is released where the page last had
// the button, so the page is not left with it pressed.
func (v *View) play(ctx context.Context, d *drag, conn *cdpConn, base time.Time) {
	type sent struct {
		method string
		reply  <-chan cdpReply
	}
	var waiting []sent
	// settle checks the answers that have come, or with wait all of them.
	settle := func(ctx context.Context, wait bool) error {
		for len(waiting) > 0 {
			var r cdpReply
			if wait {
				select {
				case r = <-waiting[0].reply:
				case <-ctx.Done():
					return ctx.Err()
				}
			} else {
				select {
				case r = <-waiting[0].reply:
				default:
					return nil
				}
			}
			method := waiting[0].method
			waiting = waiting[1:]
			if r.err != nil {
				return fmt.Errorf("%s: %w", method, r.err)
			}
		}
		return nil
	}
	var at dragEvent
	perform := func(ctx context.Context, ev dragEvent) error {
		commands, err := inputCommands(Input{Type: ev.kind, X: ev.x, Y: ev.y})
		if err != nil {
			return err
		}
		for _, c := range commands {
			reply, err := conn.send(ctx, c.method, c.params)
			if err != nil {
				return err
			}
			waiting = append(waiting, sent{c.method, reply})
		}
		at = ev
		return settle(ctx, false)
	}
	err := replay(ctx, base, d.events, func(ev dragEvent) error { return perform(ctx, ev) })
	finish, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	if at.kind == "down" || at.kind == "move" {
		err = errors.Join(err, perform(finish, dragEvent{kind: "up", x: at.x, y: at.y}))
	}
	err = errors.Join(err, settle(finish, true))
	cancel()
	conn.close()
	v.mu.Lock()
	lifted := d.lifted
	v.mu.Unlock()
	if err != nil && !lifted {
		slog.Warn("browser drag ended without its up", "tab", d.tab, "error", err)
	}
	d.err = err
	close(d.done)
	d.cancel()
	v.unhold()
}

// replay performs a drag's events, which arrive in time order, each at base
// plus its t, so the page gets them at the spacing the finger made. An event
// that arrives after its time is performed at once and every later event
// moves back by as much, so the spacing after it is kept. It returns after
// performing the up, on the first failure, when ctx ends, or when no event
// comes for dragIdle.
func replay(ctx context.Context, base time.Time, events <-chan dragEvent, perform func(dragEvent) error) error {
	idle := time.NewTimer(dragIdle)
	defer idle.Stop()
	var shift time.Duration
	for {
		var ev dragEvent
		select {
		case ev = <-events:
		case <-idle.C:
			return fmt.Errorf("no move or up for %s", dragIdle)
		case <-ctx.Done():
			return ctx.Err()
		}
		due := base.Add(time.Duration(ev.t)*time.Millisecond + shift)
		if wait := time.Until(due); wait > 0 {
			timer := time.NewTimer(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			}
		} else {
			shift -= wait
		}
		if err := perform(ev); err != nil {
			return err
		}
		if ev.kind == "up" {
			return nil
		}
		idle.Reset(dragIdle)
	}
}
