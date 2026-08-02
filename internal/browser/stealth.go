package browser

import (
	"context"

	"github.com/chromedp/cdproto/page"
)

// stealthScript masks common automation fingerprints in the page's JavaScript
// environment. It runs at document start on every navigation via
// page.AddScriptToEvaluateOnNewDocument. It only hides automation markers of
// our own browser; it does not attempt to solve or extract CAPTCHA tokens.
const stealthScript = `
(function () {
	try {
		Object.defineProperty(navigator, 'webdriver', { get: () => undefined });

		if (!window.chrome) {
			window.chrome = {
				runtime: { connect: () => ({}), sendMessage: () => {}, id: undefined },
				loadTimes: () => ({}),
				csi: () => ({})
			};
		}

		Object.defineProperty(navigator, 'languages', { get: () => ['en-US', 'en'] });
		Object.defineProperty(navigator, 'hardwareConcurrency', { get: () => 8 });

		const fakePlugins = [
			{ name: 'Chrome PDF Plugin', filename: 'internal-pdf-viewer', description: 'Portable Document Format' },
			{ name: 'Chrome PDF Viewer', filename: 'mhjfbmdgcfjbbpaeojofohoefgiehjai', description: '' },
			{ name: 'Native Client', filename: 'internal-nacl-plugin', description: '' }
		];
		Object.defineProperty(navigator, 'plugins', {
			get: () => {
				const list = fakePlugins.slice();
				list.item = (i) => list[i] || null;
				list.namedItem = (n) => list.find((p) => p.name === n) || null;
				list.refresh = () => {};
				return list;
			}
		});

		if (navigator.permissions && navigator.permissions.query) {
			const originalQuery = navigator.permissions.query.bind(navigator.permissions);
			navigator.permissions.query = (p) =>
				p && p.name === 'notifications'
					? Promise.resolve({ state: Notification.permission === 'granted' ? 'granted' : 'prompt' })
					: originalQuery(p);
		}
	} catch (e) {
		// never break the page because masking failed
	}
})();
`

// installStealthScript registers the fingerprint mask on the session's page
// context so it runs before any future navigation. It must be called after the
// target is attached (i.e. inside a chromedp.Run on the page context).
func installStealthScript(ctx context.Context) error {
	if err := page.Enable().Do(ctx); err != nil {
		return err
	}
	_, err := page.AddScriptToEvaluateOnNewDocument(stealthScript).Do(ctx)
	return err
}
