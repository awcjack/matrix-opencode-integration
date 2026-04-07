package commands

import (
	"context"
	"fmt"
	"strings"

	"github.com/personal/matrix-opencode-integration/internal/opencode"
	"github.com/personal/matrix-opencode-integration/internal/session"
)

// Handler processes bot commands
type Handler struct {
	sessionMgr *session.Manager
	ocClient   *opencode.Client
}

// NewHandler creates a new command handler
func NewHandler(sessionMgr *session.Manager, ocClient *opencode.Client) *Handler {
	return &Handler{
		sessionMgr: sessionMgr,
		ocClient:   ocClient,
	}
}

// CommandResult represents the result of a command execution
type CommandResult struct {
	Message   string
	IsError   bool
	IsCommand bool // true if the input was a command
}

// Parse checks if the input is a command and executes it
func (h *Handler) Parse(ctx context.Context, input, roomID, threadID string) *CommandResult {
	input = strings.TrimSpace(input)

	// Commands start with !
	if !strings.HasPrefix(input, "!") {
		return &CommandResult{IsCommand: false}
	}

	parts := strings.Fields(input)
	if len(parts) == 0 {
		return &CommandResult{IsCommand: false}
	}

	cmd := strings.ToLower(parts[0])
	args := parts[1:]

	switch cmd {
	case "!help":
		return h.handleHelp()
	case "!new", "!newsession":
		return h.handleNewSession(ctx, roomID, threadID)
	case "!provider", "!setprovider":
		return h.handleSetProvider(ctx, roomID, threadID, args)
	case "!providers", "!listproviders":
		return h.handleListProviders(ctx)
	case "!model", "!setmodel":
		return h.handleSetModel(ctx, roomID, threadID, args)
	case "!agent", "!setagent":
		return h.handleSetAgent(ctx, roomID, threadID, args)
	case "!agents", "!listagents":
		return h.handleListAgents(ctx)
	case "!session", "!status":
		return h.handleSessionStatus(ctx, roomID, threadID)
	case "!sessions":
		return h.handleListSessions(ctx, roomID)
	case "!switch":
		return h.handleSwitchSession(ctx, roomID, threadID, args)
	default:
		return &CommandResult{
			Message:   fmt.Sprintf("Unknown command: %s. Use !help for available commands.", cmd),
			IsError:   true,
			IsCommand: true,
		}
	}
}

func (h *Handler) handleHelp() *CommandResult {
	help := `**OpenCode Bot Commands**

**Session Management:**
• !new / !newsession - Start a new OpenCode session
• !session / !status - Show current session info
• !sessions - List all sessions (from OpenCode)
• !switch <session_id> - Switch to a specific session

**Model & Agent:**
• !model <provider/model> - Set model (e.g., !model local-claude/local-claude-opus)
• !provider <name> - Set provider only (uses provider's default model)
• !providers - List available providers
• !agent <name> - Switch to a different agent
• !agents - List available agents

**Help:**
• !help - Show this help message

**Usage:**
Simply send a message (without a command prefix) to chat with OpenCode.
Each thread maintains its own session. Use !new to start fresh.`

	return &CommandResult{
		Message:   help,
		IsCommand: true,
	}
}

func (h *Handler) handleNewSession(ctx context.Context, roomID, threadID string) *CommandResult {
	session, err := h.sessionMgr.CreateNewSession(ctx, roomID, threadID)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to create new session: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	return &CommandResult{
		Message: fmt.Sprintf("✓ Created new session: `%s`\nProvider: %s\nAgent: %s",
			session.OpenCodeSessionID,
			valueOrDefault(session.Provider, h.sessionMgr.GetDefaultProvider(), "(default)"),
			valueOrDefault(session.Agent, h.sessionMgr.GetDefaultAgent(), "(default)")),
		IsCommand: true,
	}
}

func (h *Handler) handleSetProvider(ctx context.Context, roomID, threadID string, args []string) *CommandResult {
	if len(args) == 0 {
		return &CommandResult{
			Message:   "Usage: !provider <provider_name>\nUse !providers to list available providers.",
			IsError:   true,
			IsCommand: true,
		}
	}

	providerName := args[0]

	// Verify provider exists
	providerResp, err := h.ocClient.GetProviders(ctx)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to verify provider: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	found := false
	for _, p := range providerResp.All {
		if strings.EqualFold(p.ID, providerName) || strings.EqualFold(p.Name, providerName) {
			providerName = p.ID // Use the actual ID
			found = true
			break
		}
	}

	if !found {
		return &CommandResult{
			Message:   fmt.Sprintf("Provider '%s' not found. Use !providers to list available providers.", providerName),
			IsError:   true,
			IsCommand: true,
		}
	}

	// Ensure session exists
	_, err = h.sessionMgr.GetOrCreateSession(ctx, roomID, threadID)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to get session: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	h.sessionMgr.SetProvider(roomID, threadID, providerName)

	return &CommandResult{
		Message:   fmt.Sprintf("Provider set to: %s", providerName),
		IsCommand: true,
	}
}

func (h *Handler) handleSetModel(ctx context.Context, roomID, threadID string, args []string) *CommandResult {
	if len(args) == 0 {
		return &CommandResult{
			Message:   "Usage: !model <provider/model>\nExample: !model local-claude/local-claude-opus",
			IsError:   true,
			IsCommand: true,
		}
	}

	model := args[0]

	// Validate format: should contain a slash
	if !strings.Contains(model, "/") {
		return &CommandResult{
			Message:   fmt.Sprintf("Invalid model format '%s'. Use provider/model format (e.g., local-claude/local-claude-opus)", model),
			IsError:   true,
			IsCommand: true,
		}
	}

	// Ensure session exists
	_, err := h.sessionMgr.GetOrCreateSession(ctx, roomID, threadID)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to get session: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	// Store the full model string (provider/model format)
	h.sessionMgr.SetProvider(roomID, threadID, model)

	return &CommandResult{
		Message:   fmt.Sprintf("Model set to: %s", model),
		IsCommand: true,
	}
}

func (h *Handler) handleListProviders(ctx context.Context) *CommandResult {
	providerResp, err := h.ocClient.GetProviders(ctx)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to list providers: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	if len(providerResp.All) == 0 {
		return &CommandResult{
			Message:   "No providers available.",
			IsCommand: true,
		}
	}

	// Build connected providers set for quick lookup
	connectedSet := make(map[string]bool)
	for _, c := range providerResp.Connected {
		connectedSet[c] = true
	}

	var sb strings.Builder
	sb.WriteString("**Available Providers:**\n")

	// Show connected providers first
	sb.WriteString("\n*Connected:*\n")
	for _, p := range providerResp.All {
		if connectedSet[p.ID] {
			defaultMark := ""
			if providerResp.Default != nil && providerResp.Default.ID == p.ID {
				defaultMark = " (default)"
			}
			sb.WriteString(fmt.Sprintf("• `%s` - %s%s\n", p.ID, p.Name, defaultMark))
		}
	}

	// Show available but not connected
	hasDisconnected := false
	for _, p := range providerResp.All {
		if !connectedSet[p.ID] {
			if !hasDisconnected {
				sb.WriteString("\n*Not connected:*\n")
				hasDisconnected = true
			}
			sb.WriteString(fmt.Sprintf("• `%s` - %s\n", p.ID, p.Name))
		}
	}

	return &CommandResult{
		Message:   sb.String(),
		IsCommand: true,
	}
}

func (h *Handler) handleSetAgent(ctx context.Context, roomID, threadID string, args []string) *CommandResult {
	if len(args) == 0 {
		return &CommandResult{
			Message:   "Usage: !agent <agent_name>\nUse !agents to list available agents.",
			IsError:   true,
			IsCommand: true,
		}
	}

	agentName := args[0]

	// Verify agent exists
	agents, err := h.ocClient.GetAgents(ctx)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to verify agent: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	found := false
	for _, a := range agents {
		if strings.EqualFold(a.ID, agentName) || strings.EqualFold(a.Name, agentName) {
			agentName = a.ID // Use the actual ID
			found = true
			break
		}
	}

	if !found {
		return &CommandResult{
			Message:   fmt.Sprintf("Agent '%s' not found. Use !agents to list available agents.", agentName),
			IsError:   true,
			IsCommand: true,
		}
	}

	// Ensure session exists
	_, err = h.sessionMgr.GetOrCreateSession(ctx, roomID, threadID)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to get session: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	h.sessionMgr.SetAgent(roomID, threadID, agentName)

	return &CommandResult{
		Message:   fmt.Sprintf("✓ Agent set to: %s", agentName),
		IsCommand: true,
	}
}

func (h *Handler) handleListAgents(ctx context.Context) *CommandResult {
	agents, err := h.ocClient.GetAgents(ctx)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to list agents: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	if len(agents) == 0 {
		return &CommandResult{
			Message:   "No agents available.",
			IsCommand: true,
		}
	}

	var sb strings.Builder
	sb.WriteString("**Available Agents:**\n")
	for _, a := range agents {
		sb.WriteString(fmt.Sprintf("• `%s` - %s\n", a.ID, a.Name))
	}

	return &CommandResult{
		Message:   sb.String(),
		IsCommand: true,
	}
}

func (h *Handler) handleSessionStatus(ctx context.Context, roomID, threadID string) *CommandResult {
	session, exists := h.sessionMgr.GetSession(roomID, threadID)
	if !exists {
		return &CommandResult{
			Message:   "No active session. Send a message to start one, or use !new.",
			IsCommand: true,
		}
	}

	// Fetch latest session info from OpenCode for up-to-date title
	title := session.Title
	if ocSession, err := h.ocClient.GetSession(ctx, session.OpenCodeSessionID); err == nil {
		title = ocSession.Title
	}

	var sb strings.Builder
	sb.WriteString("**Current Session:**\n")
	sb.WriteString(fmt.Sprintf("• Session ID: `%s`\n", session.OpenCodeSessionID))
	if title != "" {
		sb.WriteString(fmt.Sprintf("• Title: %s\n", title))
	}
	sb.WriteString(fmt.Sprintf("• Provider: %s\n", valueOrDefault(session.Provider, h.sessionMgr.GetDefaultProvider(), "(default)")))
	sb.WriteString(fmt.Sprintf("• Agent: %s\n", valueOrDefault(session.Agent, h.sessionMgr.GetDefaultAgent(), "(default)")))

	if threadID != "" {
		sb.WriteString(fmt.Sprintf("• Thread: `%s`\n", threadID))
	} else {
		sb.WriteString("• Thread: (main room)\n")
	}

	return &CommandResult{
		Message:   sb.String(),
		IsCommand: true,
	}
}

func (h *Handler) handleListSessions(ctx context.Context, roomID string) *CommandResult {
	// Fetch all sessions from OpenCode server
	ocSessions, err := h.ocClient.ListSessions(ctx)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to list sessions: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	if len(ocSessions) == 0 {
		return &CommandResult{
			Message:   "No sessions found on OpenCode server.",
			IsCommand: true,
		}
	}

	// Get current session for this context
	currentSession, _ := h.sessionMgr.GetSession(roomID, "")

	var sb strings.Builder
	sb.WriteString("**Available Sessions:**\n")
	for _, s := range ocSessions {
		title := s.Title
		if title == "" {
			title = "(untitled)"
		}
		current := ""
		if currentSession != nil && currentSession.OpenCodeSessionID == s.ID {
			current = " ← current"
		}
		sb.WriteString(fmt.Sprintf("• `%s` - %s%s\n", s.ID, title, current))
	}
	sb.WriteString("\nUse `!switch <session_id>` to switch to a session.")

	return &CommandResult{
		Message:   sb.String(),
		IsCommand: true,
	}
}

func (h *Handler) handleSwitchSession(ctx context.Context, roomID, threadID string, args []string) *CommandResult {
	if len(args) == 0 {
		return &CommandResult{
			Message:   "Usage: !switch <session_id>\nUse !sessions to list available sessions.",
			IsError:   true,
			IsCommand: true,
		}
	}

	sessionID := args[0]

	session, err := h.sessionMgr.SwitchToSession(ctx, roomID, threadID, sessionID)
	if err != nil {
		return &CommandResult{
			Message:   fmt.Sprintf("Failed to switch session: %v", err),
			IsError:   true,
			IsCommand: true,
		}
	}

	title := session.Title
	if title == "" {
		title = "(untitled)"
	}

	return &CommandResult{
		Message: fmt.Sprintf("Switched to session: `%s`\nTitle: %s\nProvider: %s\nAgent: %s",
			session.OpenCodeSessionID,
			title,
			valueOrDefault(session.Provider, h.sessionMgr.GetDefaultProvider(), "(default)"),
			valueOrDefault(session.Agent, h.sessionMgr.GetDefaultAgent(), "(default)")),
		IsCommand: true,
	}
}

func valueOrDefault(value, defaultValue, suffix string) string {
	if value != "" {
		return value
	}
	if defaultValue != "" {
		return defaultValue + suffix
	}
	return "(not set)"
}
