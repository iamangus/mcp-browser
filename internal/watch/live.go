package watch

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// PageProvider exposes the live streamer to the browser pages of active
// sessions without creating new ones.
type PageProvider interface {
	LookupPage(sessionID string) (context.Context, bool)
	Sessions() []string
	// TryPageCapture grants the streamer a single screenshot permit for the
	// session's page, failing when a tool call holds or waits for the page so a
	// navigation cannot be starved of the renderer. toolWaiting reports whether
	// a tool is blocked waiting, for diagnostics.
	TryPageCapture(sessionID string) (acquired, toolWaiting bool)
	// ReleasePageCapture returns the permit after the screenshot completes.
	ReleasePageCapture(sessionID string)
}

type LiveFrame struct {
	SessionID string    `json:"sessionId"`
	Type      string    `json:"type,omitempty"`
	Timestamp time.Time `json:"timestamp"`
	Mime      string    `json:"mime"`
	Image     string    `json:"image"`
}

type HubOptions struct {
	Interval time.Duration
	Quality  int
	Logger   *slog.Logger
}

type LiveHub struct {
	mu       sync.Mutex
	pages    PageProvider
	interval time.Duration
	quality  int
	logger   *slog.Logger
	subs     map[string]map[chan *LiveFrame]struct{}
	stop     map[string]chan struct{}
	trigger  map[string]chan struct{}
}

func NewHub(pages PageProvider, opts HubOptions) *LiveHub {
	if opts.Interval <= 0 {
		opts.Interval = 400 * time.Millisecond
	}
	if opts.Quality < 1 || opts.Quality > 100 {
		opts.Quality = 60
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	return &LiveHub{
		pages:    pages,
		interval: opts.Interval,
		quality:  opts.Quality,
		logger:   opts.Logger,
		subs:     make(map[string]map[chan *LiveFrame]struct{}),
		stop:     make(map[string]chan struct{}),
		trigger:  make(map[string]chan struct{}),
	}
}

func (h *LiveHub) Sessions() []string {
	if h.pages == nil {
		return nil
	}
	return h.pages.Sessions()
}

func (h *LiveHub) HasPage(sessionID string) bool {
	if h.pages == nil {
		return false
	}
	_, ok := h.pages.LookupPage(sessionID)
	return ok
}

// Subscribe registers a viewer for a session's live stream. The capture loop
// starts on the first subscriber and stops when the last one leaves. The
// returned channel yields frames; the returned function must be called to
// unsubscribe when the viewer disconnects.
func (h *LiveHub) Subscribe(sessionID string) (<-chan *LiveFrame, func()) {
	h.mu.Lock()
	if h.subs[sessionID] == nil {
		h.subs[sessionID] = make(map[chan *LiveFrame]struct{})
	}
	// Keep only one pending frame. A slow viewer must not retain a queue of
	// base64-encoded screenshots and grow the process indefinitely.
	ch := make(chan *LiveFrame, 1)
	h.subs[sessionID][ch] = struct{}{}
	start := len(h.subs[sessionID]) == 1
	h.mu.Unlock()

	if start {
		h.startStreamer(sessionID)
	}
	return ch, func() { h.unsubscribe(sessionID, ch) }
}

func (h *LiveHub) unsubscribe(sessionID string, ch chan *LiveFrame) {
	h.mu.Lock()
	stopStreamer := false
	if subs, ok := h.subs[sessionID]; ok {
		delete(subs, ch)
		close(ch)
		if len(subs) == 0 {
			delete(h.subs, sessionID)
			stopStreamer = true
		}
	}
	h.mu.Unlock()

	if stopStreamer {
		h.stopStreamer(sessionID)
	}
}

func (h *LiveHub) startStreamer(sessionID string) {
	h.mu.Lock()
	if _, ok := h.stop[sessionID]; ok {
		h.mu.Unlock()
		return
	}
	stop := make(chan struct{})
	trigger := make(chan struct{}, 1)
	h.stop[sessionID] = stop
	h.trigger[sessionID] = trigger
	subscribers := len(h.subs[sessionID])
	h.mu.Unlock()

	h.logger.Info("live stream started", "session", sessionID, "subscribers", subscribers)
	go h.streamLoop(sessionID, stop, trigger)
}

func (h *LiveHub) stopStreamer(sessionID string) {
	h.mu.Lock()
	stop := h.stop[sessionID]
	delete(h.stop, sessionID)
	delete(h.trigger, sessionID)
	h.mu.Unlock()

	if stop != nil {
		close(stop)
		h.logger.Info("live stream stopped", "session", sessionID)
	}
}

// maxConsecutiveCaptureFailures stops a streamer after this many back-to-back
// screenshot failures so a stuck capture path cannot spin forever while the
// process keeps allocating memory.
const maxConsecutiveCaptureFailures = 10

// liveStatsInterval is how often per-stream throughput is logged.
const liveStatsInterval = 30 * time.Second

func (h *LiveHub) streamLoop(sessionID string, stop chan struct{}, trigger chan struct{}) {
	// navEvent carries page-level navigation/load events. Per-resource network
	// events (Network.loadingFinished) are deliberately NOT watched: they fire
	// once per resource, and on a heavy page they would flood the capture loop
	// with back-to-back screenshot commands against a renderer that is already
	// busy composing the page, which starves an in-flight navigation.
	navEvent := make(chan struct{}, 1)
	markNav := func() {
		select {
		case navEvent <- struct{}{}:
		default:
		}
	}

	failures := 0
	statsSince := time.Now()
	statsFrames := 0
	statsBytes := int64(0)
	statsBusySkips := 0
	statsCaptureFailures := 0
	statsCaptureTotal := time.Duration(0)
	statsCaptureMax := time.Duration(0)
	logStats := func(now time.Time) {
		if statsFrames == 0 {
			statsSince = now
			return
		}
		elapsed := now.Sub(statsSince).Seconds()
		if elapsed <= 0 {
			elapsed = 1
		}
		h.logger.Info("live stream stats",
			"session", sessionID,
			"frames", statsFrames,
			"bytes_kb", statsBytes/1024,
			"fps", round2(float64(statsFrames)/elapsed),
			"busy_skips", statsBusySkips,
			"capture_failures", statsCaptureFailures,
			"avg_capture_ms", round2(float64(statsCaptureTotal/time.Millisecond)/float64(statsFrames)),
			"max_capture_ms", round2(float64(statsCaptureMax/time.Millisecond)),
		)
		statsSince = now
		statsFrames = 0
		statsBytes = 0
		statsBusySkips = 0
		statsCaptureFailures = 0
		statsCaptureTotal = 0
		statsCaptureMax = 0
	}

	// captureAndTrack takes one screenshot and records the result. It returns
	// the delay before the next capture attempt and ok=false when the streamer
	// should stop (page gone or too many failures).
	//
	// The screenshot permit comes from the page lease, so a capture is skipped
	// entirely while a tool call holds or waits for the page (e.g. during a
	// navigation); a timed-out Go context does not cancel the screenshot
	// command already executing inside Chromium, so captures never contend with
	// a tool. After a failure the next attempt is backed off.
	captureAndTrack := func(ctx context.Context, cancel context.CancelFunc) (time.Duration, bool) {
		if h.pages != nil {
			acquired, toolWaiting := h.pages.TryPageCapture(sessionID)
			if !acquired {
				statsBusySkips++
				h.logger.Debug("live capture skipped", "session", sessionID, "tool_waiting", toolWaiting)
				return h.interval, true
			}
			defer h.pages.ReleasePageCapture(sessionID)
		}
		captureStart := time.Now()
		n, err := h.capture(ctx, sessionID)
		captureDur := time.Since(captureStart)
		if err != nil {
			statsCaptureFailures++
			if errors.Is(err, context.Canceled) {
				// A canceled page context means the tab's target was destroyed
				// (e.g. the renderer crashed). Count it toward the failure
				// threshold so a dead page stops the streamer loudly instead of
				// retrying silently forever, but log at Debug to avoid noise
				// when an ordinary navigation aborts an in-flight screenshot.
				failures++
				h.logger.Debug("live capture canceled", "session", sessionID, "consecutive_failures", failures)
			} else {
				failures++
				h.logger.Warn("live capture failed",
					"session", sessionID,
					"error", err,
					"consecutive_failures", failures,
					"capture_ms", round2(float64(captureDur/time.Millisecond)),
				)
			}
			if failures >= maxConsecutiveCaptureFailures {
				h.logger.Error("live stream stopped: repeated capture failures",
					"session", sessionID,
					"consecutive_failures", failures,
				)
				h.broadcast(sessionID, &LiveFrame{SessionID: sessionID, Type: "idle", Timestamp: time.Now()})
				cancel()
				h.stopStreamer(sessionID)
				return 0, false
			}
			// Back off after a failure so a busy renderer gets time to finish
			// the command that is still running inside it.
			delay := time.Duration(failures) * h.interval
			if delay > maxCaptureBackoff {
				delay = maxCaptureBackoff
			}
			return delay, true
		}
		failures = 0
		statsFrames++
		statsBytes += int64(n)
		statsCaptureTotal += captureDur
		if captureDur > statsCaptureMax {
			statsCaptureMax = captureDur
		}
		if time.Since(statsSince) >= liveStatsInterval {
			logStats(time.Now())
		}
		return h.interval, true
	}

	// Re-acquire the page so the stream survives page recreation. The
	// context is cancelled when the page closes; loop back and retry.
	missingSince := time.Time{}
	for {
		pageCtx, ok := h.pages.LookupPage(sessionID)
		if !ok {
			if missingSince.IsZero() {
				missingSince = time.Now()
			}
			if time.Since(missingSince) >= 5*time.Second {
				h.broadcast(sessionID, &LiveFrame{SessionID: sessionID, Type: "idle", Timestamp: time.Now()})
				h.logger.Warn("live stream ended: no browser page for session", "session", sessionID)
				return
			}
			select {
			case <-stop:
				return
			case <-time.After(1 * time.Second):
				continue
			}
		}
		missingSince = time.Time{}

		ctx, cancel := context.WithCancel(pageCtx)
		chromedp.ListenTarget(ctx, func(ev any) {
			switch ev.(type) {
			case *page.EventFrameNavigated, *page.EventLoadEventFired:
				markNav()
			}
		})
		delay, ok := captureAndTrack(ctx, cancel)
		if !ok {
			return
		}

		// A timer that is only re-armed after the previous capture completes.
		// Unlike a ticker, a slow capture cannot leave a queued tick that fires
		// immediately afterwards, stacking screenshot commands back-to-back.
		for {
			timer := time.NewTimer(delay)
			select {
			case <-stop:
				timer.Stop()
				cancel()
				return
			case <-ctx.Done():
				timer.Stop()
				cancel()
				goto reacquire
			case <-timer.C:
				delay, ok = captureAndTrack(ctx, cancel)
				if !ok {
					timer.Stop()
					return
				}
			case <-navEvent:
				timer.Stop()
				delay, ok = captureAndTrack(ctx, cancel)
				if !ok {
					return
				}
			case <-trigger:
				timer.Stop()
				delay, ok = captureAndTrack(ctx, cancel)
				if !ok {
					return
				}
			}
		}
	reacquire:
	}
}

// maxCaptureBackoff caps how long the live streamer waits after a failed
// capture before trying again, giving a busy renderer time to finish the
// screenshot command that is still executing inside Chromium.
const maxCaptureBackoff = 5 * time.Second

func round2(v float64) float64 {
	return float64(int(v*100)) / 100
}

// StreamCount returns the number of sessions currently being captured.
func (h *LiveHub) StreamCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

func (h *LiveHub) capture(pageCtx context.Context, sessionID string) (int, error) {
	ctx, cancel := context.WithTimeout(pageCtx, 5*time.Second)
	defer cancel()

	mime := "image/png"
	if h.quality < 100 {
		mime = "image/jpeg"
	}
	var buf []byte
	err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
		var err error
		buf, err = page.CaptureScreenshot().
			WithFromSurface(true).
			WithFormat(page.CaptureScreenshotFormatJpeg).
			WithQuality(int64(h.quality)).
			Do(ctx)
		return err
	}))
	if err != nil {
		return 0, err
	}
	if len(buf) == 0 {
		return 0, fmt.Errorf("empty screenshot")
	}
	h.broadcast(sessionID, &LiveFrame{
		SessionID: sessionID,
		Timestamp: time.Now(),
		Mime:      mime,
		Image:     base64.StdEncoding.EncodeToString(buf),
	})
	return len(buf), nil
}

func (h *LiveHub) broadcast(sessionID string, frame *LiveFrame) {
	h.mu.Lock()
	subs := make([]chan *LiveFrame, 0, len(h.subs[sessionID]))
	for ch := range h.subs[sessionID] {
		subs = append(subs, ch)
	}
	h.mu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- frame:
		default:
			// Replace a stale frame with the newest one without growing the
			// channel or blocking the capture loop.
			select {
			case <-ch:
			default:
			}
			select {
			case ch <- frame:
			default:
			}
		}
	}
}
