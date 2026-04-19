package appservice

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"sync"
)

// Event represents a Matrix event pushed by the homeserver
type Event struct {
	EventID        string                 `json:"event_id"`
	RoomID         string                 `json:"room_id"`
	Sender         string                 `json:"sender"`
	Type           string                 `json:"type"`
	StateKey       *string                `json:"state_key,omitempty"`
	Content        map[string]interface{} `json:"content"`
	OriginServerTS int64                  `json:"origin_server_ts"`
	Unsigned       map[string]interface{} `json:"unsigned,omitempty"`
}

// Transaction represents a batch of events from the homeserver
type Transaction struct {
	Events    []Event `json:"events"`
	Ephemeral []Event `json:"ephemeral,omitempty"`
}

// EventHandler is called for each received event
type EventHandler func(ctx context.Context, event *Event)

// Server implements the Application Service HTTP API
type Server struct {
	hsToken   string
	asToken   string
	botUserID string
	handler   EventHandler
	server    *http.Server

	// Track processed transactions for idempotency
	txnMu        sync.Mutex
	processedTxn map[string]bool

	// Long-lived context for async event handlers; cancelled on Stop.
	baseCtx    context.Context
	cancelBase context.CancelFunc

	// In-flight async handlers, awaited during shutdown.
	wg sync.WaitGroup

	// Per-room serialization: each room's value is the "done" channel of
	// the most recently dispatched event. New events chain after it so
	// that events within the same room run sequentially while different
	// rooms run in parallel.
	roomMu    sync.Mutex
	roomTails map[string]chan struct{}
}

// NewServer creates a new Application Service server
func NewServer(hsToken, asToken, botUserID string, handler EventHandler) *Server {
	return &Server{
		hsToken:      hsToken,
		asToken:      asToken,
		botUserID:    botUserID,
		handler:      handler,
		processedTxn: make(map[string]bool),
		roomTails:    make(map[string]chan struct{}),
	}
}

// Start starts the AS HTTP server
func (s *Server) Start(ctx context.Context, addr string) error {
	// Derive a long-lived context for async event handlers. This is
	// independent of any individual HTTP request's context so streaming
	// work can outlive Synapse's transaction timeout.
	s.baseCtx, s.cancelBase = context.WithCancel(context.Background())

	mux := http.NewServeMux()

	// Application Service API endpoints
	mux.HandleFunc("PUT /_matrix/app/v1/transactions/{txnId}", s.handleTransaction)
	mux.HandleFunc("GET /_matrix/app/v1/users/{userId}", s.handleUserQuery)
	mux.HandleFunc("GET /_matrix/app/v1/rooms/{roomAlias}", s.handleRoomQuery)
	mux.HandleFunc("POST /_matrix/app/v1/ping", s.handlePing)

	// Legacy endpoints (fallback)
	mux.HandleFunc("PUT /transactions/{txnId}", s.handleTransaction)
	mux.HandleFunc("GET /users/{userId}", s.handleUserQuery)
	mux.HandleFunc("GET /rooms/{roomAlias}", s.handleRoomQuery)

	// Health check
	mux.HandleFunc("GET /health", s.handleHealth)

	s.server = &http.Server{
		Addr:    addr,
		Handler: s.authMiddleware(mux),
	}

	log.Printf("Application Service server starting on %s", addr)

	errChan := make(chan error, 1)
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errChan <- err
		}
	}()

	select {
	case err := <-errChan:
		return err
	case <-ctx.Done():
		return s.server.Shutdown(context.Background())
	}
}

// Stop gracefully stops the server. It first stops accepting new HTTP
// requests, then waits for in-flight async event handlers to finish (or
// for ctx to expire), and finally cancels the base context to signal
// any remaining handlers to abort.
func (s *Server) Stop(ctx context.Context) error {
	var shutdownErr error
	if s.server != nil {
		shutdownErr = s.server.Shutdown(ctx)
	}

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
	}

	if s.cancelBase != nil {
		s.cancelBase()
	}
	return shutdownErr
}

// authMiddleware verifies the hs_token from the homeserver
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Health endpoint doesn't require auth
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		auth := r.Header.Get("Authorization")
		if auth == "" {
			// Also check query parameter for legacy support
			auth = "Bearer " + r.URL.Query().Get("access_token")
		}

		expectedAuth := "Bearer " + s.hsToken
		if auth != expectedAuth {
			s.sendError(w, http.StatusForbidden, "M_FORBIDDEN", "Invalid hs_token")
			return
		}

		next.ServeHTTP(w, r)
	})
}

// handleTransaction receives events from the homeserver
func (s *Server) handleTransaction(w http.ResponseWriter, r *http.Request) {
	log.Printf("Received transaction request: %s %s", r.Method, r.URL.Path)
	txnID := r.PathValue("txnId")
	if txnID == "" {
		// Try legacy path parsing
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) > 0 {
			txnID = parts[len(parts)-1]
		}
	}

	// Check if we've already processed this transaction (idempotency)
	s.txnMu.Lock()
	if s.processedTxn[txnID] {
		s.txnMu.Unlock()
		// Already processed, return success
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{})
		return
	}
	s.txnMu.Unlock()

	var txn Transaction
	if err := json.NewDecoder(r.Body).Decode(&txn); err != nil {
		log.Printf("Failed to decode transaction: %v", err)
		s.sendError(w, http.StatusBadRequest, "M_BAD_JSON", "Invalid JSON")
		return
	}

	log.Printf("Transaction %s contains %d events", txnID, len(txn.Events))

	// Dispatch events for asynchronous processing. We must ack the
	// transaction promptly (well under Synapse's ~30s timeout) so that
	// long-running event handlers (e.g. streaming OpenCode responses)
	// do not block delivery of subsequent transactions and do not get
	// their context cancelled mid-stream when the HTTP request ends.
	for i := range txn.Events {
		event := &txn.Events[i]

		// Skip events from the bot itself
		if event.Sender == s.botUserID {
			continue
		}

		if s.handler != nil {
			s.dispatchEvent(event)
		}
	}

	// Mark transaction as processed. We do this after dispatch (but
	// before returning 200) so that a homeserver retry caused by a
	// dropped 200 response is treated as already-handled rather than
	// reprocessing the same events.
	s.txnMu.Lock()
	s.processedTxn[txnID] = true
	// Cleanup old transactions (keep last 1000)
	if len(s.processedTxn) > 1000 {
		count := 0
		for k := range s.processedTxn {
			if count > 500 {
				break
			}
			delete(s.processedTxn, k)
			count++
		}
	}
	s.txnMu.Unlock()

	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{})
}

// dispatchEvent runs the user-supplied handler in its own goroutine
// using the server-lifetime context. Events within the same room are
// serialized via a chained "tail" channel so ordering is preserved;
// events in different rooms run in parallel.
func (s *Server) dispatchEvent(event *Event) {
	done := make(chan struct{})
	roomID := event.RoomID

	s.roomMu.Lock()
	prev := s.roomTails[roomID]
	s.roomTails[roomID] = done
	s.roomMu.Unlock()

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer close(done)

		if prev != nil {
			select {
			case <-prev:
			case <-s.baseCtx.Done():
				return
			}
		}

		s.handler(s.baseCtx, event)

		// If we're still the tail for this room, remove the entry to
		// avoid unbounded growth of roomTails.
		s.roomMu.Lock()
		if s.roomTails[roomID] == done {
			delete(s.roomTails, roomID)
		}
		s.roomMu.Unlock()
	}()
}

// handleUserQuery responds to user existence queries
func (s *Server) handleUserQuery(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("userId")
	if userID == "" {
		parts := strings.Split(r.URL.Path, "/")
		if len(parts) > 0 {
			userID = parts[len(parts)-1]
		}
	}

	// Only claim our bot user exists
	if userID == s.botUserID {
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{})
		return
	}

	s.sendError(w, http.StatusNotFound, "M_NOT_FOUND", "User not found")
}

// handleRoomQuery responds to room alias queries
func (s *Server) handleRoomQuery(w http.ResponseWriter, r *http.Request) {
	// We don't manage any room aliases
	s.sendError(w, http.StatusNotFound, "M_NOT_FOUND", "Room alias not found")
}

// handlePing responds to connectivity tests
func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{})
}

// handleHealth returns health status
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"healthy": true,
	})
}

// sendError sends a Matrix-formatted error response
func (s *Server) sendError(w http.ResponseWriter, status int, errcode, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"errcode": errcode,
		"error":   message,
	})
}

// GetASToken returns the AS token for making API calls
func (s *Server) GetASToken() string {
	return s.asToken
}
