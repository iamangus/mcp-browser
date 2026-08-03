package server

import (
	"encoding/json"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// crashDump is a Chromium crashpad/breakpad minidump found on disk.
type crashDump struct {
	Name    string    `json:"name"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modtime"`

	path string
}

// findCrashDumps locates Chromium minidump files under the given roots.
// Chromium's crash handler writes *.dmp files under "Crashpad" (newer) or
// "Crash Reports" (older) directories inside the browser's user-data-dir;
// chromedp creates that directory under os.TempDir(), and Chromium may also
// fall back to $HOME/.config. Only .dmp files beneath such a directory are
// collected so unrelated files are never served.
func findCrashDumps(roots []string) []crashDump {
	dumps := make(map[string]crashDump)
	for _, root := range roots {
		if root == "" {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				// Bound the walk: crash dirs sit a few levels under the root.
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil || strings.Count(rel, string(os.PathSeparator)) > 8 {
					return fs.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(d.Name(), ".dmp") {
				return nil
			}
			if !strings.Contains(path, "Crashpad") && !strings.Contains(path, "Crash Reports") {
				return nil
			}
			info, infoErr := d.Info()
			if infoErr != nil {
				return nil
			}
			if _, exists := dumps[d.Name()]; !exists {
				dumps[d.Name()] = crashDump{
					Name:    d.Name(),
					Size:    info.Size(),
					ModTime: info.ModTime(),
					path:    path,
				}
			}
			return nil
		})
	}
	list := make([]crashDump, 0, len(dumps))
	for _, d := range dumps {
		list = append(list, d)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ModTime.After(list[j].ModTime) })
	return list
}

func crashDumpRoots() []string {
	home, _ := os.UserHomeDir()
	return []string{os.TempDir(), home}
}

// crashdumpsMux exposes Chromium crash minidumps for download so renderer
// crashes in containerized deployments (no shell access) can be diagnosed
// offline. Mounted only when DEBUG_PPROF is enabled. roots defaults to the
// standard Chromium dump locations; tests may pass their own.
func crashdumpsMux(logger *slog.Logger, roots ...string) http.Handler {
	if len(roots) == 0 {
		roots = crashDumpRoots()
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		dumps := findCrashDumps(roots)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(dumps); err != nil {
			logger.Error("failed to encode crash dump list", "error", err)
		}
	})
	mux.HandleFunc("GET /{name}", func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if name == "" || name != filepath.Base(name) || !strings.HasSuffix(name, ".dmp") {
			http.Error(w, "invalid dump name", http.StatusBadRequest)
			return
		}
		for _, d := range findCrashDumps(roots) {
			if d.Name == name {
				w.Header().Set("Content-Type", "application/octet-stream")
				w.Header().Set("Content-Disposition", "attachment; filename="+name)
				http.ServeFile(w, r, d.path)
				return
			}
		}
		http.Error(w, "dump not found", http.StatusNotFound)
	})
	return mux
}
