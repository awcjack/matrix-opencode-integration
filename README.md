# Matrix-OpenCode Integration

A Matrix integration that provides an interface to interact with [OpenCode](https://opencode.ai) via the Matrix protocol. Users can chat with OpenCode through Matrix rooms, with session persistence per thread and streaming response support.

## Features

- **Two operating modes**:
  - **Application Service** (default): Push-based, instant event delivery, lower homeserver load
  - **Bot mode** (fallback): Polling-based, works with public homeservers like matrix.org
- **Thread-based sessions**: Each Matrix thread maintains its own OpenCode session
- **Streaming responses**: Real-time response streaming with MSC4357 Live Messages support
- **User whitelist**: Only authorized users can interact with the bot
- **Provider/Agent switching**: Dynamically switch between different AI providers and agents
- **Session management**: Switch between sessions, view session history

## Prerequisites

- Go 1.22 or later
- Access to an OpenCode server
- For Application Service mode: Admin access to a Matrix homeserver (Synapse, Dendrite, etc.)
- For Bot mode: A Matrix account with access token

## Quick Start

### Application Service Mode (Self-hosted Matrix)

```bash
# 1. Build
go build -o matrix-opencode ./cmd/matrix-opencode

# 2. Generate registration file
export AS_HOMESERVER_DOMAIN=matrix.example.com
export AS_PUBLIC_URL=http://localhost:8080
./matrix-opencode --generate-registration

# 3. Copy registration.yaml to your homeserver and restart it

# 4. Create config.json (see config.example.json)

# 5. Run
./matrix-opencode -config config.json
```

### Bot Mode (matrix.org or other public servers)

```bash
# 1. Build
go build -o matrix-opencode ./cmd/matrix-opencode

# 2. Get access token (see SETUP.md)

# 3. Create config.json with mode: "bot"

# 4. Run
./matrix-opencode -config config.json
```

## Configuration

### Application Service Mode (Default)

```json
{
  "mode": "appservice",
  "matrix": {
    "homeserver": "https://matrix.example.com"
  },
  "appservice": {
    "registration_path": "/path/to/registration.yaml",
    "listen_address": ":8080",
    "homeserver_domain": "example.com",
    "sender_localpart": "opencode-bot"
  },
  "opencode": {
    "server_url": "http://localhost:4096"
  },
  "whitelist": ["@your-user:example.com"]
}
```

### Bot Mode

```json
{
  "mode": "bot",
  "matrix": {
    "homeserver": "https://matrix.org",
    "user_id": "@opencode-bot:matrix.org",
    "access_token": "YOUR_ACCESS_TOKEN"
  },
  "opencode": {
    "server_url": "http://localhost:4096"
  },
  "whitelist": ["@your-user:matrix.org"]
}
```

### Environment Variables

```bash
# Mode
MATRIX_MODE=appservice  # or "bot"

# Matrix
MATRIX_HOMESERVER=https://matrix.example.com
MATRIX_USER_ID=@opencode-bot:matrix.org  # bot mode only
MATRIX_ACCESS_TOKEN=your_token           # bot mode only

# Application Service (appservice mode)
AS_REGISTRATION_PATH=/path/to/registration.yaml
AS_LISTEN_ADDRESS=:8080
AS_HOMESERVER_DOMAIN=example.com
AS_SENDER_LOCALPART=opencode-bot

# OpenCode
OPENCODE_SERVER_URL=http://localhost:4096
OPENCODE_PASSWORD=optional_password

# Whitelist
MATRIX_WHITELIST=@user1:example.com,@user2:example.com
```

## Bot Commands

| Command | Description |
|---------|-------------|
| `!help` | Show help message |
| `!new` / `!newsession` | Start a new OpenCode session |
| `!session` / `!status` | Show current session info (including title) |
| `!sessions` | List all available sessions from OpenCode |
| `!switch <id>` | Switch to a specific session |
| `!provider <name>` | Switch to a different provider |
| `!providers` | List available providers |
| `!agent <name>` | Switch to a different agent |
| `!agents` | List available agents |

## Architecture

```
matrix-opencode-integration/
├── cmd/matrix-opencode/        # Main entry point
├── internal/
│   ├── appservice/             # Application Service API implementation
│   │   ├── registration.go     # Registration YAML generator
│   │   ├── server.go           # AS HTTP server
│   │   └── client.go           # Matrix API client for AS
│   ├── config/                 # Configuration handling
│   ├── matrix/                 # Matrix handlers and adapters
│   │   ├── handler.go          # Unified event handler
│   │   ├── as_adapter.go       # AS client adapter
│   │   └── bot_adapter.go      # Bot client adapter
│   ├── opencode/               # OpenCode API client
│   ├── session/                # Session management
│   └── commands/               # Bot command handlers
├── config.example.json         # AS mode config example
├── config.bot-mode.example.json # Bot mode config example
└── SETUP.md                    # Detailed setup guide
```

## How It Works

### Application Service Mode

```
Matrix Homeserver
       │
       │ PUT /_matrix/app/v1/transactions/{txnId}
       ↓
┌──────────────────┐         ┌─────────────────┐
│  Matrix-OpenCode │ ──────→ │  OpenCode       │
│  Integration     │ ←────── │  Server         │
└──────────────────┘   SSE   └─────────────────┘
       │
       │ Matrix Client-Server API
       ↓
Matrix Homeserver (send messages)
```

### Bot Mode

```
Matrix Homeserver
       ↑↓
       │ /sync (polling)
       │
┌──────────────────┐         ┌─────────────────┐
│  Matrix-OpenCode │ ──────→ │  OpenCode       │
│  Integration     │ ←────── │  Server         │
└──────────────────┘   SSE   └─────────────────┘
```

## Streaming Support

The integration supports real-time streaming of OpenCode responses using:

- **MSC4357 Live Messages**: Messages include `org.matrix.msc4357.live` flag during streaming
- **Message editing**: Responses are updated via `m.replace` relations
- **Visual cursor**: Shows `▌` while streaming, removed when complete

Clients that support MSC4357 will show a streaming indicator. Older clients see normal message edits.

## Detailed Setup Guide

For comprehensive setup instructions, see **[SETUP.md](./SETUP.md)**:

- Application Service registration and homeserver configuration
- Bot account setup and access token retrieval
- OpenCode server configuration
- Troubleshooting guide

## License

MIT
