package browser

import (
	"context"

	"github.com/chromedp/cdproto/inspector"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

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
