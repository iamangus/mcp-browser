package watch

import (
	"sort"
	"sync"
	"time"
)

type Snapshot struct {
	SessionID string    `json:"sessionId"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Image     []byte    `json:"image"`
	ToolName  string    `json:"toolName"`
	Timestamp time.Time `json:"timestamp"`
}

type SessionSummary struct {
	SessionID string    `json:"sessionId"`
	URL       string    `json:"url"`
	Title     string    `json:"title"`
	Image     []byte    `json:"image"`
	Timestamp time.Time `json:"timestamp"`
	Count     int       `json:"count"`
}

type Store struct {
	mu            sync.RWMutex
	snapshots     map[string][]*Snapshot
	maxPerSession int
	totalBytes    int
	subscribers   map[chan *Snapshot]struct{}
}

const maxTotalBytes = 256 * 1024 * 1024

func NewStore(maxPerSession int) *Store {
	if maxPerSession < 1 {
		maxPerSession = 50
	}
	return &Store{
		snapshots:     make(map[string][]*Snapshot),
		maxPerSession: maxPerSession,
		subscribers:   make(map[chan *Snapshot]struct{}),
	}
}

func (s *Store) Save(snapshot *Snapshot) {
	s.mu.Lock()
	hist := s.snapshots[snapshot.SessionID]
	hist = append(hist, snapshot)
	s.totalBytes += snapshotSize(snapshot)
	if len(hist) > s.maxPerSession {
		s.totalBytes -= snapshotSize(hist[0])
		hist = hist[len(hist)-s.maxPerSession:]
	}
	s.snapshots[snapshot.SessionID] = hist
	s.evictOldestLocked()
	s.mu.Unlock()

	s.broadcast(snapshot)
}

func snapshotSize(snapshot *Snapshot) int {
	if snapshot == nil {
		return 0
	}
	return len(snapshot.Image)
}

func (s *Store) evictOldestLocked() {
	for s.totalBytes > maxTotalBytes {
		var oldestSession string
		var oldest time.Time
		for sessionID, hist := range s.snapshots {
			if len(hist) == 0 {
				continue
			}
			if oldestSession == "" || hist[0].Timestamp.Before(oldest) {
				oldestSession = sessionID
				oldest = hist[0].Timestamp
			}
		}
		if oldestSession == "" {
			return
		}
		hist := s.snapshots[oldestSession]
		s.totalBytes -= snapshotSize(hist[0])
		if len(hist) == 1 {
			delete(s.snapshots, oldestSession)
		} else {
			s.snapshots[oldestSession] = hist[1:]
		}
	}
}

func (s *Store) Get(sessionID string) ([]*Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hist, ok := s.snapshots[sessionID]
	return hist, ok
}

func (s *Store) List() []*SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*SessionSummary, 0, len(s.snapshots))
	for _, hist := range s.snapshots {
		if len(hist) == 0 {
			continue
		}
		last := hist[len(hist)-1]
		out = append(out, &SessionSummary{
			SessionID: last.SessionID,
			URL:       last.URL,
			Title:     last.Title,
			Image:     last.Image,
			Timestamp: last.Timestamp,
			Count:     len(hist),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out
}

func (s *Store) Subscribe() chan *Snapshot {
	ch := make(chan *Snapshot, 1)
	s.mu.Lock()
	s.subscribers[ch] = struct{}{}
	s.mu.Unlock()
	return ch
}

func (s *Store) Unsubscribe(ch chan *Snapshot) {
	s.mu.Lock()
	delete(s.subscribers, ch)
	s.mu.Unlock()
	close(ch)
}

func (s *Store) broadcast(snapshot *Snapshot) {
	s.mu.RLock()
	subs := make([]chan *Snapshot, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subs = append(subs, ch)
	}
	s.mu.RUnlock()

	for _, ch := range subs {
		select {
		case ch <- snapshot:
		default:
		}
	}
}
