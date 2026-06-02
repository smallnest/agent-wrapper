package memory

import (
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/smallnest/agent-wrapper/types"
)

type storedSession struct {
	session types.Session
	mu      sync.RWMutex
}

// MemorySessionStore is a concurrent-safe in-memory SessionStore.
type MemorySessionStore struct {
	mu       sync.RWMutex
	sessions map[string]*storedSession
}

// New creates an empty MemorySessionStore.
func New() *MemorySessionStore {
	return &MemorySessionStore{
		sessions: make(map[string]*storedSession),
	}
}

// Create creates a new session with a UUID v4.
func (s *MemorySessionStore) Create() (*types.Session, error) {
	session := types.NewSession()
	stored := &storedSession{session: *session}

	s.mu.Lock()
	s.sessions[session.ID] = stored
	s.mu.Unlock()

	return clone(session), nil
}

// Get returns a copy of the session by ID.
// Returns *SessionNotFoundError if the session does not exist.
func (s *MemorySessionStore) Get(id string) (*types.Session, error) {
	s.mu.RLock()
	stored, ok := s.sessions[id]
	s.mu.RUnlock()

	if !ok {
		return nil, &types.SessionNotFoundError{ID: id}
	}

	stored.mu.RLock()
	defer stored.mu.RUnlock()
	return clone(&stored.session), nil
}

// Save atomically updates the session. It deep-copies Messages from the
// provided session and updates UpdatedAt.
func (s *MemorySessionStore) Save(session *types.Session) error {
	s.mu.RLock()
	stored, ok := s.sessions[session.ID]
	s.mu.RUnlock()

	if !ok {
		return &types.SessionNotFoundError{ID: session.ID}
	}

	stored.mu.Lock()
	defer stored.mu.Unlock()

	stored.session.Messages = slices.Clone(session.Messages)
	stored.session.Metadata = maps.Clone(session.Metadata)
	stored.session.UpdatedAt = time.Now()

	return nil
}

// Delete removes a session by ID.
func (s *MemorySessionStore) Delete(id string) error {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()
	return nil
}

// List returns summaries of all sessions, ordered by creation time.
func (s *MemorySessionStore) List() []*types.SessionSummary {
	s.mu.RLock()
	defer s.mu.RUnlock()

	summaries := make([]*types.SessionSummary, 0, len(s.sessions))
	for _, stored := range s.sessions {
		stored.mu.RLock()
		summaries = append(summaries, &types.SessionSummary{
			ID:           stored.session.ID,
			MessageCount: len(stored.session.Messages),
			CreatedAt:    stored.session.CreatedAt,
			UpdatedAt:    stored.session.UpdatedAt,
		})
		stored.mu.RUnlock()
	}
	return summaries
}

func clone(s *types.Session) *types.Session {
	return &types.Session{
		ID:        s.ID,
		Messages:  slices.Clone(s.Messages),
		CreatedAt: s.CreatedAt,
		UpdatedAt: s.UpdatedAt,
		Metadata:  maps.Clone(s.Metadata),
	}
}
