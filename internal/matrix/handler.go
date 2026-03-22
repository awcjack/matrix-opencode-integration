package matrix

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/personal/matrix-opencode-integration/internal/appservice"
	"github.com/personal/matrix-opencode-integration/internal/commands"
	"github.com/personal/matrix-opencode-integration/internal/config"
	"github.com/personal/matrix-opencode-integration/internal/opencode"
	"github.com/personal/matrix-opencode-integration/internal/session"
)

// MatrixClient is an interface for sending messages to Matrix
type MatrixClient interface {
	SendMessage(ctx context.Context, roomID, message string) (string, error)
	SendLiveMessage(ctx context.Context, roomID, threadID, message string) (string, error)
	EditMessage(ctx context.Context, roomID, eventID, newContent string, isLive bool) error
	SendReply(ctx context.Context, roomID, threadID, replyTo, message string, isNotice bool) (string, error)
	SetTyping(ctx context.Context, roomID string, typing bool, timeoutMS int) error
	GetBotUserID() string
}

// Handler handles Matrix events and coordinates with OpenCode
type Handler struct {
	client          MatrixClient
	ocClient        *opencode.Client
	streamingClient *opencode.StreamingClient
	sessionMgr      *session.Manager
	cmdHandler      *commands.Handler
	cfg             *config.Config

	// Track messages being streamed
	streamingMu      sync.Mutex
	streamingMsgs    map[string]*StreamingMessage // sessionID -> streaming message
	pendingResponses map[string]chan struct{}     // sessionID -> done channel

	// Track the handler start time to ignore old events
	startTime time.Time
}

// StreamingMessage tracks a message being streamed
type StreamingMessage struct {
	RoomID    string
	ThreadID  string
	EventID   string // The message event we're editing
	Content   strings.Builder
	LastEdit  time.Time
	IsLive    bool // Whether the message is still streaming (MSC4357)
}

// NewHandler creates a new event handler
func NewHandler(client MatrixClient, ocClient *opencode.Client, cfg *config.Config) *Handler {
	sessionMgr := session.NewManager(ocClient, cfg.DefaultProvider, cfg.DefaultAgent)
	streamingClient := opencode.NewStreamingClient(ocClient)

	return &Handler{
		client:           client,
		ocClient:         ocClient,
		streamingClient:  streamingClient,
		sessionMgr:       sessionMgr,
		cmdHandler:       commands.NewHandler(sessionMgr, ocClient),
		cfg:              cfg,
		streamingMsgs:    make(map[string]*StreamingMessage),
		pendingResponses: make(map[string]chan struct{}),
		startTime:        time.Now(),
	}
}

// StartEventLoop starts the OpenCode SSE event loop
func (h *Handler) StartEventLoop(ctx context.Context) error {
	return h.streamingClient.StartEventLoop(ctx)
}

// HandleEvent processes a Matrix event (from either AS or bot mode)
func (h *Handler) HandleEvent(ctx context.Context, event *appservice.Event) {
	log.Printf("HandleEvent: type=%s sender=%s room=%s", event.Type, event.Sender, event.RoomID)

	// Only handle m.room.message events
	if event.Type != "m.room.message" {
		// Handle invites
		if event.Type == "m.room.member" {
			h.handleMemberEvent(ctx, event)
		}
		return
	}

	// Ignore events from before handler started
	if event.OriginServerTS < h.startTime.UnixMilli() {
		log.Printf("Ignoring old event (ts=%d < start=%d): %s", event.OriginServerTS, h.startTime.UnixMilli(), event.EventID)
		return
	}

	// Ignore messages from the bot itself
	if event.Sender == h.client.GetBotUserID() {
		return
	}

	// Check whitelist
	if !h.cfg.IsUserWhitelisted(event.Sender) {
		log.Printf("Ignoring message from non-whitelisted user: %s", event.Sender)
		return
	}

	// Extract message content
	msgType, _ := event.Content["msgtype"].(string)
	if msgType != "m.text" {
		return
	}

	body, _ := event.Content["body"].(string)
	if body == "" {
		return
	}

	// Extract thread info
	threadID := ""
	if relatesTo, ok := event.Content["m.relates_to"].(map[string]interface{}); ok {
		if relType, _ := relatesTo["rel_type"].(string); relType == "m.thread" {
			threadID, _ = relatesTo["event_id"].(string)
		}
	}

	roomID := event.RoomID
	eventID := event.EventID

	log.Printf("Received message from %s in %s (thread: %s): %s",
		event.Sender, roomID, threadID, truncate(body, 50))

	// Check if it's a command
	result := h.cmdHandler.Parse(ctx, body, roomID, threadID)
	if result.IsCommand {
		h.sendReply(ctx, roomID, threadID, eventID, result.Message, result.IsError)
		return
	}

	// It's a regular message - send to OpenCode
	h.handleOpenCodeMessage(ctx, roomID, threadID, eventID, body)
}

// handleMemberEvent handles membership events (invites)
func (h *Handler) handleMemberEvent(ctx context.Context, event *appservice.Event) {
	membership, _ := event.Content["membership"].(string)
	stateKey := ""
	if event.StateKey != nil {
		stateKey = *event.StateKey
	}

	// Auto-join rooms we're invited to
	if membership == "invite" && stateKey == h.client.GetBotUserID() {
		log.Printf("Invited to room %s by %s, auto-joining...", event.RoomID, event.Sender)
		// Note: In AS mode, we don't need to explicitly join
		// The homeserver handles this for us
	}
}

// handleOpenCodeMessage sends a message to OpenCode and streams the response
func (h *Handler) handleOpenCodeMessage(ctx context.Context, roomID, threadID, replyTo, message string) {
	// Get or create session
	sess, err := h.sessionMgr.GetOrCreateSession(ctx, roomID, threadID)
	if err != nil {
		h.sendReply(ctx, roomID, threadID, replyTo, "Failed to create session: "+err.Error(), true)
		return
	}

	// Send typing indicator
	h.client.SetTyping(ctx, roomID, true, 30000)
	defer h.client.SetTyping(ctx, roomID, false, 0)

	// Set up streaming callback
	doneChan := make(chan struct{})
	h.streamingMu.Lock()
	h.pendingResponses[sess.OpenCodeSessionID] = doneChan
	h.streamingMu.Unlock()

	// Initialize streaming message state
	streamMsg := &StreamingMessage{
		RoomID:   roomID,
		ThreadID: threadID,
		IsLive:   true,
	}

	h.streamingMu.Lock()
	h.streamingMsgs[sess.OpenCodeSessionID] = streamMsg
	h.streamingMu.Unlock()

	// Register callback for streaming updates
	h.streamingClient.RegisterCallback(sess.OpenCodeSessionID, func(text string) {
		h.handleStreamChunk(ctx, sess.OpenCodeSessionID, text)
	})

	defer func() {
		h.streamingClient.UnregisterCallback(sess.OpenCodeSessionID)
		h.streamingMu.Lock()
		delete(h.streamingMsgs, sess.OpenCodeSessionID)
		delete(h.pendingResponses, sess.OpenCodeSessionID)
		h.streamingMu.Unlock()
	}()

	// Send message to OpenCode (async for streaming)
	err = h.ocClient.SendMessageAsync(ctx, sess.OpenCodeSessionID, message, sess.Provider, sess.Agent)
	if err != nil {
		// Fall back to synchronous if async fails
		resp, syncErr := h.ocClient.SendMessage(ctx, sess.OpenCodeSessionID, message, sess.Provider, sess.Agent)
		if syncErr != nil {
			h.sendReply(ctx, roomID, threadID, replyTo, "Failed to send message: "+syncErr.Error(), true)
			return
		}

		// Extract text from response
		var responseText string
		for _, part := range resp.Parts {
			if part.Type == "text" {
				responseText += part.Text
			}
		}

		if responseText == "" {
			responseText = "(No response)"
		}

		h.sendReply(ctx, roomID, threadID, replyTo, responseText, false)
		return
	}

	// Wait for streaming to complete with timeout
	select {
	case <-doneChan:
		// Streaming completed
	case <-time.After(5 * time.Minute):
		log.Printf("Streaming timeout for session %s", sess.OpenCodeSessionID)
	case <-ctx.Done():
		return
	}

	// Finalize the streaming message
	h.finalizeStreamingMessage(ctx, sess.OpenCodeSessionID)

	// Send final message if we haven't sent anything yet
	h.streamingMu.Lock()
	finalContent := streamMsg.Content.String()
	eventID := streamMsg.EventID
	h.streamingMu.Unlock()

	if finalContent == "" {
		finalContent = "(No response received)"
	}

	if eventID == "" {
		h.sendReply(ctx, roomID, threadID, replyTo, finalContent, false)
	}
}

// handleStreamChunk handles incoming stream chunks
func (h *Handler) handleStreamChunk(ctx context.Context, sessionID, text string) {
	h.streamingMu.Lock()
	streamMsg, exists := h.streamingMsgs[sessionID]
	if !exists {
		h.streamingMu.Unlock()
		return
	}

	streamMsg.Content.WriteString(text)
	currentContent := streamMsg.Content.String()
	eventID := streamMsg.EventID
	roomID := streamMsg.RoomID
	threadID := streamMsg.ThreadID
	lastEdit := streamMsg.LastEdit
	isLive := streamMsg.IsLive
	h.streamingMu.Unlock()

	// Throttle updates to avoid rate limiting
	if time.Since(lastEdit) < 500*time.Millisecond && eventID != "" {
		return
	}

	if eventID == "" {
		// Send initial message with MSC4357 live flag
		newEventID, err := h.client.SendLiveMessage(ctx, roomID, threadID, currentContent+"▌")
		if err != nil {
			log.Printf("Failed to send initial streaming message: %v", err)
			return
		}

		h.streamingMu.Lock()
		if sm, ok := h.streamingMsgs[sessionID]; ok {
			sm.EventID = newEventID
			sm.LastEdit = time.Now()
		}
		h.streamingMu.Unlock()
	} else {
		// Edit existing message with live flag
		err := h.client.EditMessage(ctx, roomID, eventID, currentContent+"▌", isLive)
		if err != nil {
			log.Printf("Failed to edit streaming message: %v", err)
		}

		h.streamingMu.Lock()
		if sm, ok := h.streamingMsgs[sessionID]; ok {
			sm.LastEdit = time.Now()
		}
		h.streamingMu.Unlock()
	}
}

// finalizeStreamingMessage sends the final edit without the live flag
func (h *Handler) finalizeStreamingMessage(ctx context.Context, sessionID string) {
	h.streamingMu.Lock()
	streamMsg, exists := h.streamingMsgs[sessionID]
	if !exists {
		h.streamingMu.Unlock()
		return
	}

	streamMsg.IsLive = false
	finalContent := streamMsg.Content.String()
	eventID := streamMsg.EventID
	roomID := streamMsg.RoomID
	h.streamingMu.Unlock()

	if eventID != "" && finalContent != "" {
		err := h.client.EditMessage(ctx, roomID, eventID, finalContent, false)
		if err != nil {
			log.Printf("Failed to finalize streaming message: %v", err)
		}
	}
}

// sendReply sends a reply to a message
func (h *Handler) sendReply(ctx context.Context, roomID, threadID, replyTo, message string, isError bool) {
	_, err := h.client.SendReply(ctx, roomID, threadID, replyTo, message, isError)
	if err != nil {
		log.Printf("Failed to send reply: %v", err)
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
