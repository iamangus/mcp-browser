package browser

import (
	"context"
	"log/slog"

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
// target. It also Error-logs renderer crashes so a wedged tab can be told
// apart from a crashed one in pod logs.
func installDialogHandler(ctx context.Context, sessionID string, logger *slog.Logger) {
	chromedp.ListenTarget(ctx, func(ev any) {
		switch ev := ev.(type) {
		case *page.EventJavascriptDialogOpening:
			logger.Warn("javascript dialog opened, auto-accepting",
				"session", sessionID,
				"dialog_type", ev.Type,
				"message", ev.Message,
				"url", ev.URL,
			)
			go func() {
				if err := chromedp.Run(ctx, chromedp.ActionFunc(func(ctx context.Context) error {
					return page.HandleJavaScriptDialog(true).Do(ctx)
				})); err != nil && ctx.Err() == nil {
					logger.Warn("failed to accept javascript dialog", "session", sessionID, "error", err)
				}
			}()
		case *inspector.EventTargetCrashed:
			logger.Error("renderer target crashed", "session", sessionID)
		}
	})
}
