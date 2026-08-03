package server

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFindCrashDumps(t *testing.T) {
	root := t.TempDir()
	dumpDir := filepath.Join(root, "chromedp-runner123", "Crashpad", "pending")
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dumpDir, "abc-123.dmp"), []byte("MINIDUMP"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a crash dir: must be ignored.
	other := filepath.Join(root, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(other, "ignore.dmp"), []byte("NOPE"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Not a .dmp file inside a crash dir: must be ignored.
	if err := os.WriteFile(filepath.Join(dumpDir, "notes.txt"), []byte("NOPE"), 0o644); err != nil {
		t.Fatal(err)
	}

	dumps := findCrashDumps([]string{root})
	if len(dumps) != 1 {
		t.Fatalf("expected 1 dump, got %d", len(dumps))
	}
	if dumps[0].Name != "abc-123.dmp" {
		t.Fatalf("unexpected dump name %q", dumps[0].Name)
	}
	if dumps[0].Size != int64(len("MINIDUMP")) {
		t.Fatalf("unexpected size %d", dumps[0].Size)
	}
}

func TestCrashdumpsHandler(t *testing.T) {
	root := t.TempDir()
	dumpDir := filepath.Join(root, "Crashpad", "completed")
	if err := os.MkdirAll(dumpDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := []byte("MINIDUMP-BYTES")
	if err := os.WriteFile(filepath.Join(dumpDir, "dead-beef.dmp"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mux := crashdumpsMux(logger, root)

	// List
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: status %d", rec.Code)
	}
	var listed []crashDump
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("list: invalid json: %v", err)
	}
	if len(listed) != 1 || listed[0].Name != "dead-beef.dmp" {
		t.Fatalf("list: unexpected dumps %+v", listed)
	}

	// Download
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/dead-beef.dmp", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("download: status %d", rec.Code)
	}
	body, _ := io.ReadAll(rec.Result().Body)
	if string(body) != string(content) {
		t.Fatalf("download: got %q, want %q", body, content)
	}

	// Missing dump
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/missing.dmp", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing dump: status %d, want 404", rec.Code)
	}

	// Traversal attempts are rejected before any file access.
	for _, name := range []string{"..%2F..%2Fetc%2Fpasswd.dmp", "..\\evil.dmp", "no-extension"} {
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/"+name, nil))
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusNotFound {
			t.Fatalf("traversal %q: status %d, want 400/404", name, rec.Code)
		}
	}
}
