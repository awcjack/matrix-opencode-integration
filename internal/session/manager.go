package session

import (
	"context"
	"sync"

	"github.com/personal/matrix-opencode-integration/internal/opencode"
)

// UserSession represents a user's session mapping to OpenCode
type UserSession struct {
	// Matrix room ID
	RoomID string

	// Matrix thread ID (event ID of the thread root)
	// Empty string means main room (not in a thread)
	ThreadID string

	// OpenCode session ID
	OpenCodeSessionID string

	// Session title (from OpenCode)
	Title string

	// Current provider (empty means use default)
	Provider string

	// Current agent (empty means use default)
	Agent string
}

// SessionKey uniquely identifies a Matrix conversation context
type SessionKey struct {
	RoomID   string
	ThreadID string
}

// Manager manages the mapping between Matrix threads and OpenCode sessions
type Manager struct {
	mu       sync.RWMutex
	sessions map[SessionKey]*UserSession
	client   *opencode.Client

	// Default provider and agent
	defaultProvider string
	defaultAgent    string
}

// NewManager creates a new session manager
func NewManager(client *opencode.Client, defaultProvider, defaultAgent string) *Manager {
	return &Manager{
		sessions:        make(map[SessionKey]*UserSession),
		client:          client,
		defaultProvider: defaultProvider,
		defaultAgent:    defaultAgent,
	}
}

// GetOrCreateSession retrieves or creates a session for the given Matrix context
func (m *Manager) GetOrCreateSession(ctx context.Context, roomID, threadID string) (*UserSession, error) {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}

	m.mu.RLock()
	session, exists := m.sessions[key]
	m.mu.RUnlock()

	if exists {
		return session, nil
	}

	// Create a new OpenCode session
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check after acquiring write lock
	if session, exists = m.sessions[key]; exists {
		return session, nil
	}

	// Create session in OpenCode
	title := "Matrix: " + roomID
	if threadID != "" {
		title += " (Thread: " + threadID + ")"
	}

	ocSession, err := m.client.CreateSession(ctx, title)
	if err != nil {
		return nil, err
	}

	session = &UserSession{
		RoomID:            roomID,
		ThreadID:          threadID,
		OpenCodeSessionID: ocSession.ID,
		Title:             ocSession.Title,
		Provider:          m.defaultProvider,
		Agent:             m.defaultAgent,
	}

	m.sessions[key] = session
	return session, nil
}

// GetSession retrieves an existing session without creating one
func (m *Manager) GetSession(roomID, threadID string) (*UserSession, bool) {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}
	m.mu.RLock()
	defer m.mu.RUnlock()
	session, exists := m.sessions[key]
	return session, exists
}

// CreateNewSession creates a new session, replacing any existing one
func (m *Manager) CreateNewSession(ctx context.Context, roomID, threadID string) (*UserSession, error) {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Create a new OpenCode session
	title := "Matrix: " + roomID
	if threadID != "" {
		title += " (Thread: " + threadID + ")"
	}

	ocSession, err := m.client.CreateSession(ctx, title)
	if err != nil {
		return nil, err
	}

	// Preserve provider/agent settings from old session if it exists
	oldSession, exists := m.sessions[key]
	provider := m.defaultProvider
	agent := m.defaultAgent
	if exists {
		provider = oldSession.Provider
		agent = oldSession.Agent
	}

	session := &UserSession{
		RoomID:            roomID,
		ThreadID:          threadID,
		OpenCodeSessionID: ocSession.ID,
		Title:             ocSession.Title,
		Provider:          provider,
		Agent:             agent,
	}

	m.sessions[key] = session
	return session, nil
}

// SwitchToSession switches the current context to an existing OpenCode session
func (m *Manager) SwitchToSession(ctx context.Context, roomID, threadID, sessionID string) (*UserSession, error) {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}

	// Fetch session info from OpenCode to verify it exists and get title
	ocSession, err := m.client.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Preserve provider/agent settings from old session if it exists
	oldSession, exists := m.sessions[key]
	provider := m.defaultProvider
	agent := m.defaultAgent
	if exists {
		provider = oldSession.Provider
		agent = oldSession.Agent
	}

	session := &UserSession{
		RoomID:            roomID,
		ThreadID:          threadID,
		OpenCodeSessionID: ocSession.ID,
		Title:             ocSession.Title,
		Provider:          provider,
		Agent:             agent,
	}

	m.sessions[key] = session
	return session, nil
}

// SetProvider updates the provider for a session
func (m *Manager) SetProvider(roomID, threadID, provider string) bool {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[key]
	if !exists {
		return false
	}

	session.Provider = provider
	return true
}

// SetAgent updates the agent for a session
func (m *Manager) SetAgent(roomID, threadID, agent string) bool {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}
	m.mu.Lock()
	defer m.mu.Unlock()

	session, exists := m.sessions[key]
	if !exists {
		return false
	}

	session.Agent = agent
	return true
}

// DeleteSession removes a session mapping
func (m *Manager) DeleteSession(roomID, threadID string) {
	key := SessionKey{RoomID: roomID, ThreadID: threadID}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, key)
}

// ListSessions returns all active sessions for a room
func (m *Manager) ListSessions(roomID string) []*UserSession {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*UserSession
	for key, session := range m.sessions {
		if key.RoomID == roomID {
			result = append(result, session)
		}
	}
	return result
}

// GetDefaultProvider returns the default provider
func (m *Manager) GetDefaultProvider() string {
	return m.defaultProvider
}

// GetDefaultAgent returns the default agent
func (m *Manager) GetDefaultAgent() string {
	return m.defaultAgent
}
