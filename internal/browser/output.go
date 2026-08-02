package browser

import (
	"log/slog"
	"strings"
	"sync"
)

// chromiumOutputTailSize bounds how much of Chromium's combined stdout+stderr
// we retain for the crash tail dump.
const chromiumOutputTailSize = 64 * 1024

// chromiumOutput captures the Chromium process's combined stdout+stderr. It
// mirrors each chunk into the app log at Debug and keeps a bounded tail buffer
// so the crash reason can be dumped at Error level when the browser process
// dies. Without this, every browser-side failure (GPU process crash, Vulkan
// init error, missing library) is invisible to the Go process's logs.
type chromiumOutput struct {
	logger *slog.Logger

	mu   sync.Mutex
	tail []byte
}

func newChromiumOutput(logger *slog.Logger) *chromiumOutput {
	return &chromiumOutput{logger: logger}
}

func (o *chromiumOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	o.tail = append(o.tail, p...)
	if len(o.tail) > chromiumOutputTailSize {
		o.tail = o.tail[len(o.tail)-chromiumOutputTailSize:]
	}
	o.mu.Unlock()
	o.logger.Debug("chromium output", "out", strings.TrimSpace(string(p)))
	return len(p), nil
}

func (o *chromiumOutput) Tail() string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return string(o.tail)
}
