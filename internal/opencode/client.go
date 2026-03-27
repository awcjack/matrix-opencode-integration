package opencode

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
)

// Client is the OpenCode API client
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// NewClient creates a new OpenCode client
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    strings.TrimSuffix(baseURL, "/"),
		username:   username,
		password:   password,
		httpClient: &http.Client{},
	}
}

// HealthResponse represents the health check response
type HealthResponse struct {
	Healthy bool   `json:"healthy"`
	Version string `json:"version"`
}

// CheckHealth verifies the OpenCode server is running
func (c *Client) CheckHealth(ctx context.Context) (*HealthResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/global/health", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health check failed: %s", resp.Status)
	}

	var health HealthResponse
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &health, nil
}

// Session represents an OpenCode session
type Session struct {
	ID       string `json:"id"`
	Title    string `json:"title,omitempty"`
	ParentID string `json:"parentID,omitempty"`
}

// Message represents a message in OpenCode
type Message struct {
	ID        string        `json:"id"`
	Role      string        `json:"role"`
	Parts     []MessagePart `json:"parts"`
	SessionID string        `json:"sessionID"`
}

// MessagePart represents a part of a message
type MessagePart struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Provider represents an OpenCode provider
type Provider struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Default bool   `json:"default,omitempty"`
}

// Agent represents an OpenCode agent
type Agent struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// StreamEvent represents a Server-Sent Event from OpenCode
type StreamEvent struct {
	Event string
	Data  string
}

// StreamCallback is called for each chunk of streamed response
type StreamCallback func(text string)

// CompletionCallback is called when a message streaming is complete
type CompletionCallback func(sessionID string)

// doRequest performs an authenticated HTTP request
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	return c.httpClient.Do(req)
}

// CreateSession creates a new OpenCode session
func (c *Client) CreateSession(ctx context.Context, title string) (*Session, error) {
	body := map[string]string{}
	if title != "" {
		body["title"] = title
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/session", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("create session failed: %s - %s", resp.Status, string(bodyBytes))
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &session, nil
}

// GetSession retrieves a session by ID
func (c *Client) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/session/"+sessionID, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get session failed: %s", resp.Status)
	}

	var session Session
	if err := json.NewDecoder(resp.Body).Decode(&session); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &session, nil
}

// ListSessions lists all sessions
func (c *Client) ListSessions(ctx context.Context) ([]Session, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/session", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list sessions failed: %s", resp.Status)
	}

	var sessions []Session
	if err := json.NewDecoder(resp.Body).Decode(&sessions); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return sessions, nil
}

// DeleteSession deletes a session
func (c *Client) DeleteSession(ctx context.Context, sessionID string) error {
	resp, err := c.doRequest(ctx, http.MethodDelete, "/session/"+sessionID, nil)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("delete session failed: %s", resp.Status)
	}

	return nil
}

// SendMessageRequest represents a message send request
type SendMessageRequest struct {
	Parts     []MessagePart `json:"parts"`
	Model     string        `json:"model,omitempty"`
	Agent     string        `json:"agent,omitempty"`
	MessageID string        `json:"messageID,omitempty"`
}

// SendMessageResponse represents the response from sending a message
type SendMessageResponse struct {
	Info  Message       `json:"info"`
	Parts []MessagePart `json:"parts"`
}

// SendMessage sends a message to a session (non-streaming)
func (c *Client) SendMessage(ctx context.Context, sessionID string, message string, model, agent string) (*SendMessageResponse, error) {
	req := SendMessageRequest{
		Parts: []MessagePart{{Type: "text", Text: message}},
		Model: model,
		Agent: agent,
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/session/"+sessionID+"/message", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("send message failed: %s - %s", resp.Status, string(bodyBytes))
	}

	var result SendMessageResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &result, nil
}

// SendMessageAsync sends a message asynchronously (for streaming)
func (c *Client) SendMessageAsync(ctx context.Context, sessionID string, message string, model, agent string) error {
	req := SendMessageRequest{
		Parts: []MessagePart{{Type: "text", Text: message}},
		Model: model,
		Agent: agent,
	}

	resp, err := c.doRequest(ctx, http.MethodPost, "/session/"+sessionID+"/prompt_async", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("send async message failed: %s - %s", resp.Status, string(bodyBytes))
	}

	return nil
}

// StreamEvents connects to the SSE event stream
func (c *Client) StreamEvents(ctx context.Context) (<-chan StreamEvent, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/event", nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Cache-Control", "no-cache")

	if c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connect to event stream: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("event stream failed: %s", resp.Status)
	}

	events := make(chan StreamEvent, 100)

	go func() {
		defer resp.Body.Close()
		defer close(events)
		log.Printf("SSE event loop started, connected to %s/event", c.baseURL)

		reader := bufio.NewReader(resp.Body)
		var currentEvent StreamEvent

		for {
			select {
			case <-ctx.Done():
				log.Printf("SSE event loop: context cancelled")
				return
			default:
			}

			line, err := reader.ReadString('\n')
			if err != nil {
				if err != io.EOF {
					log.Printf("SSE event loop error: %v", err)
				} else {
					log.Printf("SSE event loop: connection closed (EOF)")
				}
				return
			}

			line = strings.TrimSpace(line)

			if line == "" {
				// End of event
				if currentEvent.Event != "" || currentEvent.Data != "" {
					select {
					case events <- currentEvent:
					case <-ctx.Done():
						return
					}
					currentEvent = StreamEvent{}
				}
				continue
			}

			if strings.HasPrefix(line, "event:") {
				currentEvent.Event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			} else if strings.HasPrefix(line, "data:") {
				data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
				if currentEvent.Data != "" {
					currentEvent.Data += "\n"
				}
				currentEvent.Data += data
			}
		}
	}()

	return events, nil
}

// ProviderResponse represents the provider list response
type ProviderResponse struct {
	All       []Provider `json:"all"`
	Default   *Provider  `json:"default,omitempty"`
	Connected []string   `json:"connected"`
}

// GetProviders lists all available providers
func (c *Client) GetProviders(ctx context.Context) (*ProviderResponse, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/provider", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get providers failed: %s", resp.Status)
	}

	var providerResp ProviderResponse
	if err := json.NewDecoder(resp.Body).Decode(&providerResp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &providerResp, nil
}

// GetAgents lists all available agents
func (c *Client) GetAgents(ctx context.Context) ([]Agent, error) {
	resp, err := c.doRequest(ctx, http.MethodGet, "/agent", nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("get agents failed: %s", resp.Status)
	}

	var agents []Agent
	if err := json.NewDecoder(resp.Body).Decode(&agents); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return agents, nil
}

// StreamingClient provides streaming message handling
type StreamingClient struct {
	client              *Client
	mu                  sync.Mutex
	callbacks           map[string]StreamCallback // sessionID -> callback
	completionCallback  CompletionCallback        // global completion callback
	lastContent         map[string]string         // sessionID -> last content sent (for dedup)
	receivedDelta       map[string]bool           // sessionID -> whether we've received delta events
	assistantMessageIDs map[string]string         // sessionID -> current assistant messageID (to filter user messages)
}

// NewStreamingClient creates a streaming client
func NewStreamingClient(client *Client) *StreamingClient {
	return &StreamingClient{
		client:              client,
		callbacks:           make(map[string]StreamCallback),
		lastContent:         make(map[string]string),
		receivedDelta:       make(map[string]bool),
		assistantMessageIDs: make(map[string]string),
	}
}

// SetCompletionCallback sets the callback for message completion events
func (sc *StreamingClient) SetCompletionCallback(callback CompletionCallback) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.completionCallback = callback
}

// RegisterCallback registers a callback for a session's streaming response
func (sc *StreamingClient) RegisterCallback(sessionID string, callback StreamCallback) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.callbacks[sessionID] = callback
}

// UnregisterCallback removes a session's callback
func (sc *StreamingClient) UnregisterCallback(sessionID string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	delete(sc.callbacks, sessionID)
	delete(sc.lastContent, sessionID)
	delete(sc.receivedDelta, sessionID)
	delete(sc.assistantMessageIDs, sessionID)
}

// getCallback retrieves a callback for a session
func (sc *StreamingClient) getCallback(sessionID string) (StreamCallback, bool) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	cb, ok := sc.callbacks[sessionID]
	return cb, ok
}

// StartEventLoop starts processing events from the OpenCode server
func (sc *StreamingClient) StartEventLoop(ctx context.Context) error {
	events, err := sc.client.StreamEvents(ctx)
	if err != nil {
		return err
	}

	go func() {
		for event := range events {
			sc.processEvent(event)
		}
	}()

	return nil
}

// processEvent handles incoming SSE events
func (sc *StreamingClient) processEvent(event StreamEvent) {
	// Parse the event based on its type
	// OpenCode sends events like "message-v2.updated", "message-v2.part.updated", "message-v2.part.delta"
	var baseData struct {
		Type       string          `json:"type"`
		Properties json.RawMessage `json:"properties"`
	}

	if err := json.Unmarshal([]byte(event.Data), &baseData); err != nil {
		return
	}

	switch baseData.Type {
	case "message-v2.part.delta", "message.part.delta":
		// Handle streaming text deltas (only for assistant messages)
		var props struct {
			SessionID string `json:"sessionID"`
			MessageID string `json:"messageID"`
			PartID    string `json:"partID"`
			Field     string `json:"field"`
			Delta     string `json:"delta"`
		}
		if err := json.Unmarshal(baseData.Properties, &props); err != nil {
			return
		}

		// Only process deltas for assistant messages
		sc.mu.Lock()
		assistantMsgID := sc.assistantMessageIDs[props.SessionID]
		sc.mu.Unlock()
		if props.MessageID != assistantMsgID {
			return // Skip non-assistant message deltas
		}

		if cb, ok := sc.getCallback(props.SessionID); ok && props.Delta != "" {
			// Mark that we've received delta events for this session
			sc.mu.Lock()
			sc.receivedDelta[props.SessionID] = true
			sc.mu.Unlock()
			cb(props.Delta)
		}

	case "message-v2.updated", "message.updated":
		// Track assistant message IDs and check for completion
		var props struct {
			SessionID string `json:"sessionID"`
			MessageID string `json:"messageID"`
			Info      struct {
				Role string `json:"role"`
				Time struct {
					Completed int64 `json:"completed,omitempty"`
				} `json:"time"`
			} `json:"info"`
		}
		if err := json.Unmarshal(baseData.Properties, &props); err != nil {
			return
		}

		// Track the current assistant message ID for this session
		if props.Info.Role == "assistant" && props.MessageID != "" {
			sc.mu.Lock()
			sc.assistantMessageIDs[props.SessionID] = props.MessageID
			sc.mu.Unlock()
		}

		// If it's an assistant message with a completed timestamp, signal completion
		if props.Info.Role == "assistant" && props.Info.Time.Completed > 0 {
			sc.mu.Lock()
			cb := sc.completionCallback
			sc.mu.Unlock()
			if cb != nil {
				cb(props.SessionID)
			}
		}

	case "message-v2.part.updated", "message.part.updated":
		// Handle part updates - extract text content (only for assistant messages)
		// Skip if we're receiving delta events (they provide the same content)
		var props struct {
			SessionID string `json:"sessionID"`
			MessageID string `json:"messageID"`
			Part      struct {
				Type    string `json:"type"`
				Text    string `json:"text"`
				Content string `json:"content"`
			} `json:"part"`
		}
		if err := json.Unmarshal(baseData.Properties, &props); err != nil {
			return
		}

		// Only process updates for assistant messages
		sc.mu.Lock()
		assistantMsgID := sc.assistantMessageIDs[props.SessionID]
		hasDeltas := sc.receivedDelta[props.SessionID]
		sc.mu.Unlock()

		if props.MessageID != assistantMsgID {
			return // Skip non-assistant message updates
		}

		// Skip if we're receiving delta events for this session
		if hasDeltas {
			return
		}

		// Only handle text parts - check both "text" and "content" fields
		text := props.Part.Text
		if text == "" {
			text = props.Part.Content
		}
		if props.Part.Type == "text" && text != "" {
			// Deduplicate: message.part.updated sends full content each time
			// Only send new content (delta) to avoid duplicates
			sc.mu.Lock()
			lastContent := sc.lastContent[props.SessionID]
			if text != lastContent && len(text) > len(lastContent) {
				// New content is longer - extract delta
				delta := text[len(lastContent):]
				sc.lastContent[props.SessionID] = text
				sc.mu.Unlock()

				if cb, ok := sc.getCallback(props.SessionID); ok {
					cb(delta)
				}
			} else {
				sc.mu.Unlock()
			}
		}
	}
}
