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

type Store struct {
	mu          sync.RWMutex
	snapshots   map[string]*Snapshot
	subscribers map[chan *Snapshot]struct{}
}

func NewStore() *Store {
	return &Store{
		snapshots:   make(map[string]*Snapshot),
		subscribers: make(map[chan *Snapshot]struct{}),
	}
}

func (s *Store) Save(snapshot *Snapshot) {
	s.mu.Lock()
	s.snapshots[snapshot.SessionID] = snapshot
	s.mu.Unlock()

	s.broadcast(snapshot)
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
