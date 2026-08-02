package browser

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/angoo/mcp-browser/internal/config"
)

// TestPageLeaseArbitration verifies the exclusive per-session lease that keeps
// live captures and tool calls from running on the same page at the same time.
func TestPageLeaseArbitration(t *testing.T) {
	m := NewManager(&config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	// A free page yields the capture permit.
	if acquired, waiting := m.TryPageCapture("s1"); !acquired || waiting {
		t.Fatalf("expected free capture permit, got acquired=%v waiting=%v", acquired, waiting)
	}
	m.ReleasePageCapture("s1")

	// A held tool lease blocks captures.
	release, _, err := m.BeginPageOp(context.Background(), "s1")
	if err != nil {
		t.Fatalf("BeginPageOp: %v", err)
	}
	if acquired, _ := m.TryPageCapture("s1"); acquired {
		t.Fatal("capture permit granted while a tool holds the page")
	}

	// A second tool call waits; captures must yield to it (tool_waiting=true).
	var wg sync.WaitGroup
	var secondWait time.Duration
	var secondErr error
	var secondRelease func()
	wg.Add(1)
	go func() {
		defer wg.Done()
		secondRelease, secondWait, secondErr = m.BeginPageOp(context.Background(), "s1")
	}()
	time.Sleep(100 * time.Millisecond)
	acquired, waiting := m.TryPageCapture("s1")
	if acquired || !waiting {
		t.Fatalf("expected capture to yield to waiting tool, got acquired=%v waiting=%v", acquired, waiting)
	}

	release()
	wg.Wait()
	if secondErr != nil {
		t.Fatalf("second BeginPageOp: %v", secondErr)
	}
	if secondWait <= 0 {
		t.Fatalf("expected the second tool to wait, got wait=%s", secondWait)
	}
	secondRelease()

	// After the tool releases, captures work again.
	if acquired, _ := m.TryPageCapture("s1"); !acquired {
		t.Fatal("capture permit not available after tool release")
	}
	m.ReleasePageCapture("s1")
}

// TestPageLeaseCancel verifies that a canceled tool wait releases the waiter
// slot so captures are not permanently blocked.
func TestPageLeaseCancel(t *testing.T) {
	m := NewManager(&config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)))

	release, _, err := m.BeginPageOp(context.Background(), "s2")
	if err != nil {
		t.Fatalf("BeginPageOp: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _, _ = m.BeginPageOp(ctx, "s2")
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()
	wg.Wait()

	// After the canceled waiter exits, a capture must still see the first
	// holder (token held) but not a waiter, and after release it is free.
	acquired, waiting := m.TryPageCapture("s2")
	if acquired || waiting {
		t.Fatalf("unexpected state after cancel, acquired=%v waiting=%v", acquired, waiting)
	}
	release()
	if acquired, _ := m.TryPageCapture("s2"); !acquired {
		t.Fatal("capture permit not available after release")
	}
	m.ReleasePageCapture("s2")
}
