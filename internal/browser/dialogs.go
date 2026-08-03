package browser

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// shmAndChromiumMem reports shared-memory usage (the kernel-wide Shmem figure
// from /proc/meminfo, which includes the container's /dev/shm that
// Chromium's GpuMemoryBuffer/SHM discardable allocator draws from) and the
// summed RSS (in MB) of all Chromium processes, plus how many processes that
// covers. It is emitted at renderer-crash time so an OOM death can be
// attributed to shm exhaustion vs. real memory pressure. Failures return
// zero/empty values so logging degrades gracefully.
func shmAndChromiumMem() (shmemKB string, chromiumRSSMB int, chromiumProcs int) {
	if data, err := os.ReadFile("/proc/meminfo"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Shmem:") {
				fields := strings.Fields(line)
				if len(fields) >= 2 {
					shmemKB = fields[1]
				}
				break
			}
		}
	}
	procs, _ := filepath.Glob("/proc/[0-9]*/statm")
	var totalKB int64
	for _, p := range procs {
		cmdline, err := os.ReadFile(filepath.Dir(p) + "/cmdline")
		if err != nil {
			continue
		}
		cmd := string(cmdline)
		if !strings.Contains(cmd, "chromium") && !strings.Contains(cmd, "chrome_crashpad") {
			continue
		}
		statm, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fields := strings.Fields(string(statm))
		if len(fields) < 2 {
			continue
		}
		residentPages, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			continue
		}
		totalKB += residentPages * 4
		chromiumProcs++
	}
	chromiumRSSMB = int(totalKB / 1024)
	return shmemKB, chromiumRSSMB, chromiumProcs
}

// installDialogHandler makes sure JavaScript dialogs (alert, confirm, prompt,
// beforeunload) can never wedge a session page. Headed Chromium renders such
// dialogs into the (invisible) X display and blocks the renderer's main thread
// until the dialog is answered; chromedp never answers on its own, so a single
// alert() would freeze the tab, every later navigation, and all live captures.
// Headless mode suppresses dialogs, which is why the hang only appears headed.
//
// The handler Warn-logs every dialog (type, message, URL) and accepts it,
// which unblocks the renderer and lets beforeunload navigations proceed.
// Commands are issued from a goroutine: listeners run on the CDP pump loop,
// so a synchronous chromedp.Run from inside the callback would deadlock the
// target.
//
// It also handles renderer crashes: the tab's target stays alive after its
// renderer dies, so the page context is never cancelled and pending commands
// would hang until their timeout while the tab shows "Aw, snap". Closing the
// page cancels its context (unblocking any in-flight command), dumps
// Chromium's stderr tail for the crash reason, and lets the next tool call
// recreate a fresh tab, so the session recovers on its own.
func installDialogHandler(ctx context.Context, sessionID string, m *BrowserManager) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			m.logger.Warn("javascript dialog opened, auto-accepting",
				"session", sessionID,
				"dialog_type", ev.Type,
				"message", ev.Message,
				"url", ev.URL,
			)
			go func() {
				if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					return page.HandleJavaScriptDialog(true).Do(ctx)
				})); err != nil && ctx.Err() == nil {
					m.logger.Warn("failed to accept javascript dialog", "session", sessionID, "error", err)
				}
			}()
		case *inspector.EventTargetCrashed:
			attrs := []any{"session", sessionID}
			shmemKB, rssMB, nProcs := shmAndChromiumMem()
			if shmemKB != "" {
				attrs = append(attrs, "shmem_kb", shmemKB)
			}
			if rssMB > 0 {
				attrs = append(attrs, "chromium_rss_mb", rssMB, "chromium_procs", nProcs)
			}
			if m.chromiumOut != nil {
				if tail := m.chromiumOut.Tail(); tail != "" {
					attrs = append(attrs, "chromium_stderr_tail", tail)
				}
			}
			m.logger.Error("renderer target crashed: closing page so the session recovers", attrs...)
			m.ClosePage(sessionID)
		}
	})
}
