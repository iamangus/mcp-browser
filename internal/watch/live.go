package watch

import (
	"context"
	"encoding/base64"
	"log/slog"
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// PageProvider exposes the live streamer to the browser pages of active
// sessions without creating new ones.
type PageProvider interface {
	LookupPage(sessionID string) (context.Context, bool)
	Sessions() []string
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
	h.mu.Unlock()

	h.logger.Info("live stream started", "session", sessionID)
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

func (h *LiveHub) streamLoop(sessionID string, stop chan struct{}, trigger chan struct{}) {
	eventCh := make(chan struct{}, 1)
	markEvent := func() {
		select {
		case eventCh <- struct{}{}:
		default:
		}
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
			case *network.EventLoadingFinished, *page.EventFrameNavigated, *page.EventLoadEventFired:
				markEvent()
			}
		})
		h.capture(ctx, sessionID)

		ticker := time.NewTicker(h.interval)
		for {
			select {
			case <-stop:
				cancel()
				ticker.Stop()
				return
			case <-ctx.Done():
				cancel()
				ticker.Stop()
				goto reacquire
			case <-ticker.C:
				h.capture(ctx, sessionID)
			case <-eventCh:
				h.capture(ctx, sessionID)
			case <-trigger:
				h.capture(ctx, sessionID)
			}
		}
	reacquire:
	}
}

func (h *LiveHub) capture(pageCtx context.Context, sessionID string) {
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
		return
	}
	if len(buf) == 0 {
		return
	}
	h.broadcast(sessionID, &LiveFrame{
		SessionID: sessionID,
		Timestamp: time.Now(),
		Mime:      mime,
		Image:     base64.StdEncoding.EncodeToString(buf),
	})
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
