package config

import (
	"encoding/json"
	"os"
)

// Mode represents the Matrix connection mode
type Mode string

const (
	// ModeAppService uses the Application Service API (push-based, requires HS admin)
	ModeAppService Mode = "appservice"
	// ModeBot uses the Client-Server API with polling (fallback, no admin required)
	ModeBot Mode = "bot"
)

// Config holds the application configuration
type Config struct {
	// Mode: "appservice" (default) or "bot"
	Mode Mode `json:"mode"`

	// Matrix configuration
	Matrix MatrixConfig `json:"matrix"`

	// Application Service configuration (used when mode=appservice)
	AppService AppServiceConfig `json:"appservice,omitempty"`

	// OpenCode configuration
	OpenCode OpenCodeConfig `json:"opencode"`

	// Whitelist of allowed Matrix user IDs
	Whitelist []string `json:"whitelist"`

	// Default provider and agent
	DefaultProvider string `json:"default_provider"`
	DefaultAgent    string `json:"default_agent"`
}

// MatrixConfig holds Matrix-specific configuration
type MatrixConfig struct {
	// Homeserver URL (e.g., https://matrix.org)
	Homeserver string `json:"homeserver"`

	// Bot user ID (e.g., @opencode-bot:matrix.org)
	UserID string `json:"user_id"`

	// Access token for authentication (only used in bot mode)
	AccessToken string `json:"access_token,omitempty"`

	// Device ID (optional, only used in bot mode)
	DeviceID string `json:"device_id,omitempty"`
}

// AppServiceConfig holds Application Service specific configuration
type AppServiceConfig struct {
	// ID is the unique identifier for this AS
	ID string `json:"id"`

	// RegistrationPath is the path to the registration YAML file
	RegistrationPath string `json:"registration_path"`

	// ListenAddress is the address to listen for HS callbacks (e.g., ":8080")
	ListenAddress string `json:"listen_address"`

	// PublicURL is the URL the homeserver uses to reach this AS
	PublicURL string `json:"public_url"`

	// HSToken is the token for the homeserver to authenticate to us
	HSToken string `json:"hs_token,omitempty"`

	// ASToken is the token for us to authenticate to the homeserver
	ASToken string `json:"as_token,omitempty"`

	// SenderLocalpart is the localpart of the bot user (e.g., "opencode-bot")
	SenderLocalpart string `json:"sender_localpart"`

	// HomeserverDomain is the domain part of the homeserver (e.g., "matrix.example.com")
	HomeserverDomain string `json:"homeserver_domain"`
}

// OpenCodeConfig holds OpenCode server configuration
type OpenCodeConfig struct {
	// Server URL (e.g., http://localhost:4096)
	ServerURL string `json:"server_url"`

	// Username for HTTP Basic Auth (default: opencode)
	Username string `json:"username,omitempty"`

	// Password for HTTP Basic Auth
	Password string `json:"password,omitempty"`
}

// Load reads configuration from a JSON file
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	// Set defaults
	cfg.setDefaults()

	// Override secrets from environment variables (allows config file + env secrets)
	cfg.applyEnvOverrides()

	return &cfg, nil
}

// applyEnvOverrides allows environment variables to override config file values
// This is useful for secrets that shouldn't be in config files
func (c *Config) applyEnvOverrides() {
	// AppService tokens from environment
	if v := os.Getenv("AS_HS_TOKEN"); v != "" {
		c.AppService.HSToken = v
	}
	if v := os.Getenv("AS_TOKEN"); v != "" {
		c.AppService.ASToken = v
	}

	// OpenCode credentials from environment
	if v := os.Getenv("OPENCODE_PASSWORD"); v != "" {
		c.OpenCode.Password = v
	}
	if v := os.Getenv("OPENCODE_USERNAME"); v != "" {
		c.OpenCode.Username = v
	}

	// Matrix access token for bot mode
	if v := os.Getenv("MATRIX_ACCESS_TOKEN"); v != "" {
		c.Matrix.AccessToken = v
	}
}

// LoadFromEnv reads configuration from environment variables
func LoadFromEnv() *Config {
	mode := Mode(os.Getenv("MATRIX_MODE"))

	cfg := &Config{
		Mode: mode,
		Matrix: MatrixConfig{
			Homeserver:  os.Getenv("MATRIX_HOMESERVER"),
			UserID:      os.Getenv("MATRIX_USER_ID"),
			AccessToken: os.Getenv("MATRIX_ACCESS_TOKEN"),
			DeviceID:    os.Getenv("MATRIX_DEVICE_ID"),
		},
		AppService: AppServiceConfig{
			ID:               os.Getenv("AS_ID"),
			RegistrationPath: os.Getenv("AS_REGISTRATION_PATH"),
			ListenAddress:    os.Getenv("AS_LISTEN_ADDRESS"),
			PublicURL:        os.Getenv("AS_PUBLIC_URL"),
			HSToken:          os.Getenv("AS_HS_TOKEN"),
			ASToken:          os.Getenv("AS_TOKEN"),
			SenderLocalpart:  os.Getenv("AS_SENDER_LOCALPART"),
			HomeserverDomain: os.Getenv("AS_HOMESERVER_DOMAIN"),
		},
		OpenCode: OpenCodeConfig{
			ServerURL: os.Getenv("OPENCODE_SERVER_URL"),
			Username:  getEnvDefault("OPENCODE_USERNAME", "opencode"),
			Password:  os.Getenv("OPENCODE_PASSWORD"),
		},
		Whitelist:       parseWhitelist(os.Getenv("MATRIX_WHITELIST")),
		DefaultProvider: os.Getenv("OPENCODE_DEFAULT_PROVIDER"),
		DefaultAgent:    os.Getenv("OPENCODE_DEFAULT_AGENT"),
	}

	cfg.setDefaults()
	return cfg
}

// setDefaults applies default values
func (c *Config) setDefaults() {
	// Default mode is appservice
	if c.Mode == "" {
		c.Mode = ModeAppService
	}

	if c.OpenCode.Username == "" {
		c.OpenCode.Username = "opencode"
	}

	if c.AppService.ID == "" {
		c.AppService.ID = "opencode-bridge"
	}

	if c.AppService.ListenAddress == "" {
		c.AppService.ListenAddress = ":8080"
	}

	if c.AppService.SenderLocalpart == "" {
		c.AppService.SenderLocalpart = "opencode-bot"
	}
}

// IsAppServiceMode returns true if running in Application Service mode
func (c *Config) IsAppServiceMode() bool {
	return c.Mode == ModeAppService
}

// IsBotMode returns true if running in bot (client API) mode
func (c *Config) IsBotMode() bool {
	return c.Mode == ModeBot
}

// GetBotUserID returns the full bot user ID
func (c *Config) GetBotUserID() string {
	if c.Matrix.UserID != "" {
		return c.Matrix.UserID
	}
	if c.AppService.SenderLocalpart != "" && c.AppService.HomeserverDomain != "" {
		return "@" + c.AppService.SenderLocalpart + ":" + c.AppService.HomeserverDomain
	}
	return ""
}

// Validate checks if the configuration is valid for the selected mode
func (c *Config) Validate() error {
	if c.IsAppServiceMode() {
		return c.validateAppServiceMode()
	}
	return c.validateBotMode()
}

func (c *Config) validateAppServiceMode() error {
	// For AS mode, we need either:
	// 1. A registration file path, OR
	// 2. HS token, AS token, and other AS config
	if c.AppService.RegistrationPath != "" {
		return nil
	}
	if c.AppService.HSToken != "" && c.AppService.ASToken != "" {
		return nil
	}
	// Will generate registration on first run
	return nil
}

func (c *Config) validateBotMode() error {
	// For bot mode, we need access token
	if c.Matrix.AccessToken == "" {
		return &ConfigError{Field: "matrix.access_token", Message: "required in bot mode"}
	}
	return nil
}

// ConfigError represents a configuration validation error
type ConfigError struct {
	Field   string
	Message string
}

func (e *ConfigError) Error() string {
	return e.Field + ": " + e.Message
}

func getEnvDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func parseWhitelist(s string) []string {
	if s == "" {
		return nil
	}
	var whitelist []string
	if err := json.Unmarshal([]byte(s), &whitelist); err != nil {
		// Try comma-separated format
		return splitAndTrim(s)
	}
	return whitelist
}

func splitAndTrim(s string) []string {
	var result []string
	for _, part := range splitString(s, ',') {
		trimmed := trimSpace(part)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func splitString(s string, sep rune) []string {
	var result []string
	var current []rune
	for _, r := range s {
		if r == sep {
			result = append(result, string(current))
			current = nil
		} else {
			current = append(current, r)
		}
	}
	result = append(result, string(current))
	return result
}

func trimSpace(s string) string {
	start := 0
	end := len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}

// IsUserWhitelisted checks if a user ID is in the whitelist
func (c *Config) IsUserWhitelisted(userID string) bool {
	if len(c.Whitelist) == 0 {
		return false // No whitelist means deny all
	}
	for _, allowed := range c.Whitelist {
		if allowed == userID || allowed == "*" {
			return true
		}
	}
	return false
}
