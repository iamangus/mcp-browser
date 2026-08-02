package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"time"

	"github.com/chromedp/cdproto/input"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

// cdpMouseButton maps the tool's button names to CDP mouse buttons.
func cdpMouseButton(button string) input.MouseButton {
	switch button {
	case "right":
		return input.Right
	case "middle":
		return input.Middle
	case "back":
		return input.Back
	case "forward":
		return input.Forward
	default:
		return input.Left
	}
}

// humanClickAtPoint returns an action that performs a human-like left click at
// coordinates supplied by get (so callers can click at a position computed by an
// earlier action in the same run) using the CDP Input domain (isTrusted=true):
// a short eased mouse path with jitter and small random delays, then real
// pressed/released events. It replaces the previous synthetic JS MouseEvent
// dispatch which bot detectors flag immediately.
func humanClickAtPoint(get func() (float64, float64)) chromedp.Action {
	return humanClickAtButton(get, input.Left, 1)
}

// humanClickAtButton is humanClickAt with an explicit button and click count.
func humanClickAtButton(get func() (float64, float64), btn input.MouseButton, clickCount int) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		x, y := get()
		fromX, fromY := readMousePos(ctx)
		if err := moveMouse(ctx, fromX, fromY, x, y); err != nil {
			return err
		}
		if clickCount < 1 {
			clickCount = 1
		}
		for i := 1; i <= clickCount; i++ {
			if err := input.DispatchMouseEvent(input.MousePressed, x, y).
				WithButton(btn).WithClickCount(int64(i)).Do(ctx); err != nil {
				return err
			}
			time.Sleep(time.Duration(40+rand.Intn(90)) * time.Millisecond)
			if err := input.DispatchMouseEvent(input.MouseReleased, x, y).
				WithButton(btn).WithClickCount(int64(i)).Do(ctx); err != nil {
				return err
			}
			if i < clickCount {
				time.Sleep(time.Duration(60+rand.Intn(100)) * time.Millisecond)
			}
		}
		writeMousePos(ctx, x, y)
		return nil
	})
}

// moveTo returns an action that moves the pointer to a position supplied by get.
func moveTo(get func() (float64, float64)) chromedp.Action {
	return moveToSteps(get, 0)
}

// moveToSteps is moveTo with an explicit step count.
func moveToSteps(get func() (float64, float64), steps int) chromedp.Action {
	return chromedp.ActionFunc(func(ctx context.Context) error {
		x, y := get()
		fromX, fromY := readMousePos(ctx)
		return moveMouseSteps(ctx, fromX, fromY, x, y, steps)
	})
}

// moveMouse moves the pointer from (fromX, fromY) to (toX, toY) in eased steps
// with jitter and small random delays via the CDP Input domain.
func moveMouse(ctx context.Context, fromX, fromY, toX, toY float64) error {
	return moveMouseSteps(ctx, fromX, fromY, toX, toY, 0)
}

func moveMouseSteps(ctx context.Context, fromX, fromY, toX, toY float64, steps int) error {
	if steps <= 0 {
		steps = autoMoveSteps(fromX, fromY, toX, toY)
	}
	if steps > 40 {
		steps = 40
	}
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		e := 1 - math.Pow(1-t, 3)
		x := fromX + (toX-fromX)*e
		y := fromY + (toY-fromY)*e
		if i < steps {
			x += (rand.Float64()*2 - 1) * 1.5
			y += (rand.Float64()*2 - 1) * 1.5
		}
		if err := input.DispatchMouseEvent(input.MouseMoved, x, y).
			WithButton(input.None).Do(ctx); err != nil {
			return err
		}
		if i < steps {
			time.Sleep(time.Duration(8+rand.Intn(24)) * time.Millisecond)
		}
	}
	writeMousePos(ctx, toX, toY)
	return nil
}

// autoMoveSteps picks a step count for a mouse path based on distance.
func autoMoveSteps(fromX, fromY, toX, toY float64) int {
	switch dist := math.Hypot(toX-fromX, toY-fromY); {
	case dist < 24:
		return 3
	case dist < 100:
		return 6
	default:
		return 10
	}
}

// elementCenterScript returns JS that scrolls the element into view and yields
// the viewport coordinates of its center.
func elementCenterScript(selector string) string {
	return fmt.Sprintf(`(function(){
		var el = document.querySelector(%q);
		if (!el) throw new Error('Element not found: ' + %q);
		el.scrollIntoView({behavior: 'smooth', block: 'center'});
		var r = el.getBoundingClientRect();
		return {x: r.left + r.width / 2, y: r.top + r.height / 2};
	})()`, selector, selector)
}

func readMousePos(ctx context.Context) (float64, float64) {
	ro, _, err := runtime.Evaluate(`({x: window._lastMouseX || 0, y: window._lastMouseY || 0})`).
		WithReturnByValue(true).Do(ctx)
	if err != nil || ro == nil || ro.Value == nil {
		return 0, 0
	}
	var p struct {
		X float64 `json:"x"`
		Y float64 `json:"y"`
	}
	if err := json.Unmarshal(ro.Value, &p); err != nil {
		return 0, 0
	}
	return p.X, p.Y
}

func writeMousePos(ctx context.Context, x, y float64) {
	_, _, _ = runtime.Evaluate(fmt.Sprintf(`window._lastMouseX=%f; window._lastMouseY=%f; true`, x, y)).
		WithReturnByValue(true).Do(ctx)
}
