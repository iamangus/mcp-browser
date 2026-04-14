package watch

import (
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

type Store struct {
	mu        sync.RWMutex
	snapshots map[string]*Snapshot
}

func NewStore() *Store {
	return &Store{
		snapshots: make(map[string]*Snapshot),
	}
}

func (s *Store) Save(snapshot *Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshots[snapshot.SessionID] = snapshot
}

func (s *Store) Get(sessionID string) (*Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap, ok := s.snapshots[sessionID]
	return snap, ok
}

func (s *Store) List() []*Snapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Snapshot, 0, len(s.snapshots))
	for _, snap := range s.snapshots {
		out = append(out, snap)
	}
	return out
}
