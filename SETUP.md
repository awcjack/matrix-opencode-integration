# Setup Guide

This guide provides detailed instructions for setting up both the Matrix bot and OpenCode server, and explains how they interact with each other.

## Table of Contents

1. [Operating Modes](#operating-modes)
2. [OpenCode Server Setup](#opencode-server-setup)
3. [Application Service Mode Setup (Recommended)](#application-service-mode-setup-recommended)
4. [Bot Mode Setup (Fallback)](#bot-mode-setup-fallback)
5. [Running the Integration](#running-the-integration)
6. [How the Integration Works](#how-the-integration-works)
7. [Troubleshooting](#troubleshooting)

---

## Operating Modes

The integration supports two modes of connecting to Matrix:

| Mode | Description | Requirements | Best For |
|------|-------------|--------------|----------|
| **Application Service** (default) | Push-based, events sent to AS | Homeserver admin access | Self-hosted Matrix servers |
| **Bot** (fallback) | Polling-based, uses /sync API | Bot account + access token | Public homeservers (matrix.org) |

### Mode Comparison

| Feature | Application Service | Bot Mode |
|---------|--------------------|---------|
| Event delivery | Push (instant) | Poll (slight delay) |
| Homeserver load | Lower | Higher |
| Setup complexity | Higher | Lower |
| Requires HS admin | Yes | No |
| Works on matrix.org | No | Yes |
| Rate limits | No (can be disabled) | Yes |

---

## OpenCode Server Setup

### Installing OpenCode

OpenCode is a terminal-based AI coding assistant. Install it using one of these methods:

```bash
# Using Homebrew (macOS/Linux)
brew install opencode

# Using npm
npm install -g opencode

# Using Go
go install github.com/opencode-ai/opencode@latest
```

### Starting the OpenCode Server

The integration requires OpenCode to run in server mode:

```bash
# Basic server (no authentication)
opencode serve

# With custom port
opencode serve --port 4096

# With custom hostname (for remote access)
opencode serve --hostname 0.0.0.0 --port 4096
```

### Server Authentication

For production deployments, enable HTTP Basic Authentication:

```bash
# Set password (username defaults to "opencode")
OPENCODE_SERVER_PASSWORD=your-secure-password opencode serve
```

### Configuring LLM Providers

```bash
# Interactive setup
opencode auth

# Or via environment
export ANTHROPIC_API_KEY=sk-ant-...
export OPENAI_API_KEY=sk-...
```

### Verifying Server Status

```bash
curl http://localhost:4096/global/health
# Returns: {"healthy":true,"version":"x.x.x"}
```

---

## Application Service Mode Setup (Recommended)

Application Service mode is the recommended approach for self-hosted Matrix servers. Events are pushed to the integration instantly, reducing homeserver load.

### Step 1: Generate Registration File

```bash
# Set required environment variables
export AS_HOMESERVER_DOMAIN=matrix.example.com
export AS_SENDER_LOCALPART=opencode-bot
export AS_PUBLIC_URL=http://your-server:8080
export AS_LISTEN_ADDRESS=:8080

# Generate registration
./matrix-opencode --generate-registration --registration-output registration.yaml
```

This creates `registration.yaml`:

```yaml
id: opencode-bridge
url: http://your-server:8080
as_token: <generated-token>
hs_token: <generated-token>
sender_localpart: opencode-bot
rate_limited: false
namespaces:
  users:
    - exclusive: true
      regex: "@opencode-bot:matrix\\.example\\.com"
  rooms: []
  aliases: []
```

### Step 2: Configure Your Homeserver

#### For Synapse

1. Copy `registration.yaml` to your Synapse config directory:
   ```bash
   cp registration.yaml /etc/synapse/appservices/opencode.yaml
   ```

2. Edit `homeserver.yaml`:
   ```yaml
   app_service_config_files:
     - /etc/synapse/appservices/opencode.yaml
   ```

3. Restart Synapse:
   ```bash
   systemctl restart synapse
   ```

#### For Dendrite

1. Edit `dendrite.yaml`:
   ```yaml
   app_service_api:
     config_files:
       - /path/to/registration.yaml
   ```

2. Restart Dendrite.

#### For Conduit

Conduit handles AS registration differently. See Conduit documentation.

### Step 3: Configure the Integration

Create `config.json`:

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
    "server_url": "http://localhost:4096",
    "password": "your-opencode-password"
  },
  "whitelist": [
    "@your-user:example.com"
  ]
}
```

Or use environment variables:

```bash
export MATRIX_MODE=appservice
export MATRIX_HOMESERVER=https://matrix.example.com
export AS_REGISTRATION_PATH=/path/to/registration.yaml
export AS_LISTEN_ADDRESS=:8080
export AS_HOMESERVER_DOMAIN=example.com
export AS_SENDER_LOCALPART=opencode-bot
export OPENCODE_SERVER_URL=http://localhost:4096
export MATRIX_WHITELIST=@your-user:example.com
```

### Step 4: Network Configuration

Ensure your homeserver can reach the AS:

```
Homeserver (matrix.example.com)
      ↓
      ↓ HTTP to AS_PUBLIC_URL
      ↓
Application Service (your-server:8080)
```

If behind a reverse proxy:

```nginx
# nginx config
location /_matrix/app/ {
    proxy_pass http://localhost:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
}
```

Update `registration.yaml` URL to match your public URL.

---

## Bot Mode Setup (Fallback)

Use bot mode when you don't have homeserver admin access (e.g., matrix.org).

### Step 1: Create a Matrix Account

1. Go to https://app.element.io
2. Create account with username like `opencode-bot`
3. Save credentials securely

### Step 2: Get Access Token

```bash
curl -X POST "https://matrix.org/_matrix/client/v3/login" \
  -H "Content-Type: application/json" \
  -d '{
    "type": "m.login.password",
    "identifier": {"type": "m.id.user", "user": "opencode-bot"},
    "password": "your-password",
    "device_id": "OPENCODE_BOT"
  }'
```

Save the `access_token` from the response.

### Step 3: Configure the Integration

Create `config.json`:

```json
{
  "mode": "bot",
  "matrix": {
    "homeserver": "https://matrix.org",
    "user_id": "@opencode-bot:matrix.org",
    "access_token": "syt_your_token_here",
    "device_id": "OPENCODE_BOT"
  },
  "opencode": {
    "server_url": "http://localhost:4096"
  },
  "whitelist": [
    "@your-user:matrix.org"
  ]
}
```

---

## Running the Integration

### Build

```bash
cd matrix-opencode-integration
go mod tidy
go build -o matrix-opencode ./cmd/matrix-opencode
```

### Start Services

1. **Start OpenCode Server:**
   ```bash
   OPENCODE_SERVER_PASSWORD=secret opencode serve --port 4096
   ```

2. **Start Matrix Integration:**
   ```bash
   ./matrix-opencode -config config.json
   ```

### Using Docker Compose

```bash
docker-compose up -d
```

---

## How the Integration Works

### Architecture (Application Service Mode)

```
┌─────────────┐         ┌──────────────────┐         ┌─────────────────┐
│   Matrix    │  push   │  Matrix-OpenCode │   API   │  OpenCode       │
│  Homeserver │ ──────→ │  Integration     │ ──────→ │  Server         │
└─────────────┘  events └──────────────────┘         └─────────────────┘
      ↑                        │                            ↑
      │                        │                            │
   Matrix CS API          SSE stream                   LLM Provider
   (send messages)        (responses)                  (Anthropic, etc)
```

### Event Flow

1. **User sends message in Matrix**
   - Homeserver pushes event to AS via `PUT /_matrix/app/v1/transactions/{txnId}`

2. **Integration processes message**
   - Check whitelist → Find/create session → Send to OpenCode

3. **OpenCode processes request**
   - `POST /session/:id/prompt_async` → SSE stream at `/event`

4. **Integration streams response**
   - SSE events → Edit Matrix message with MSC4357 live flag → Final message

### Session Management

- **Thread-based**: Each Matrix thread gets its own OpenCode session
- **Session persistence**: Thread ID → OpenCode Session ID mapping
- **Commands**: `!new` creates fresh session, `!switch` changes session

### API Endpoints Used

| Endpoint | Purpose |
|----------|---------|
| `GET /global/health` | Verify OpenCode server |
| `POST /session` | Create session |
| `POST /session/:id/prompt_async` | Send message (streaming) |
| `GET /event` | SSE stream for responses |
| `GET /provider` | List providers |
| `GET /agent` | List agents |

---

## Troubleshooting

### Application Service Issues

**"AS not receiving events"**
- Verify homeserver can reach AS URL: `curl http://your-as:8080/health`
- Check homeserver logs for AS connection errors
- Ensure registration is properly loaded (restart homeserver)
- Verify hs_token matches between registration and config

**"M_FORBIDDEN from homeserver"**
- hs_token mismatch - regenerate registration
- Registration file not properly loaded

**"Bot user doesn't exist"**
- The AS creates the bot user automatically
- Ensure sender_localpart matches registration
- Check homeserver logs for user creation errors

### Bot Mode Issues

**"Invalid access token"**
- Token may have expired - regenerate via login API
- Don't log out from Element after copying token

**"Rate limited"**
- Bot mode is subject to homeserver rate limits
- Consider Application Service mode for high volume

### OpenCode Issues

**"Connection refused"**
- Ensure OpenCode server is running: `opencode serve`
- Check port is correct

**"No providers"**
- Configure API keys: `opencode auth`

### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `dial tcp: connection refused` | OpenCode not running | Start `opencode serve` |
| `401 Unauthorized` | Invalid token | Regenerate access/hs token |
| `403 Forbidden` | Whitelist/token issue | Check whitelist and tokens |
| `M_UNKNOWN_TOKEN` | Expired Matrix token | Get new access token |

### Debug Mode

```bash
DEBUG=true ./matrix-opencode -config config.json
```

---

## Quick Start Commands

### Application Service Mode (Self-hosted)
```bash
# 1. Generate registration
export AS_HOMESERVER_DOMAIN=matrix.example.com
./matrix-opencode --generate-registration

# 2. Copy registration to homeserver & restart

# 3. Run
./matrix-opencode -config config.json
```

### Bot Mode (matrix.org)
```bash
# 1. Get token
curl -X POST "https://matrix.org/_matrix/client/v3/login" ...

# 2. Configure config.json with mode: "bot"

# 3. Run
./matrix-opencode -config config.json
```
