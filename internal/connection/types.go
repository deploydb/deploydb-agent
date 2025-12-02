// Package connection provides WebSocket connectivity to the DeployDb control plane.
package connection

import (
	"errors"
	"time"
)

// Errors
var (
	ErrNotConnected = errors.New("not connected")
	ErrClosed       = errors.New("client is closed")
)

// Message is the envelope for all WebSocket messages.
type Message struct {
	Type    string      `json:"type"`
	Payload interface{} `json:"payload"`
}

// AgentHelloPayload is sent when the agent first connects.
type AgentHelloPayload struct {
	Token           string `json:"token"`
	AgentVersion    string `json:"agent_version"`
	Hostname        string `json:"hostname"`
	OS              string `json:"os"`
	Arch            string `json:"arch"`
	PostgresVersion string `json:"postgres_version,omitempty"`
}

// WelcomePayload is received after successful authentication.
type WelcomePayload struct {
	ServerID               string `json:"server_id"`
	MetricsIntervalSeconds int    `json:"metrics_interval_seconds"`
	SigningPublicKey       string `json:"signing_public_key,omitempty"`
	CommandsEnabled        bool   `json:"commands_enabled"`
}

// ErrorPayload is received when the server rejects the connection.
type ErrorPayload struct {
	Code              string `json:"code"`
	Message           string `json:"message"`
	RetryAfterSeconds int    `json:"retry_after_seconds,omitempty"`
}

// MetricsPayload is sent periodically with collected metrics.
type MetricsPayload struct {
	Timestamp int64              `json:"timestamp"`
	Metrics   map[string]float64 `json:"metrics"`
}

// PingPayload is sent as a keepalive.
type PingPayload struct {
	Timestamp int64 `json:"timestamp"`
}

// PongPayload is the response to a ping.
type PongPayload struct {
	Timestamp int64 `json:"timestamp"`
}

// CommandPayload is received when the control plane sends a command.
type CommandPayload struct {
	ID            string                 `json:"id"`
	SignedPayload map[string]interface{} `json:"signed_payload"`
	Signature     string                 `json:"signature"`
}

// Command represents a command received from the control plane.
type Command struct {
	ID            string
	ServerID      string
	Command       string
	Params        map[string]interface{}
	Nonce         string
	Timestamp     time.Time
	Signature     string
	RawPayload    map[string]interface{}
}

// CommandResultPayload is sent after executing a command.
type CommandResultPayload struct {
	CommandID  string                 `json:"command_id"`
	Status     string                 `json:"status"` // success, failed, rejected
	Result     map[string]interface{} `json:"result,omitempty"`
	Error      string                 `json:"error,omitempty"`
	DurationMs int64                  `json:"duration_ms,omitempty"`
}

// Config holds the client configuration.
type Config struct {
	URL             string
	Token           string
	AgentVersion    string
	Hostname        string
	OS              string
	Arch            string
	PostgresVersion string

	// Reconnection settings
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64

	// Ping interval for keepalive
	PingInterval time.Duration
}

// DefaultConfig returns config with sensible defaults.
func (c Config) WithDefaults() Config {
	if c.InitialBackoff == 0 {
		c.InitialBackoff = 1 * time.Second
	}
	if c.MaxBackoff == 0 {
		c.MaxBackoff = 5 * time.Minute
	}
	if c.BackoffFactor == 0 {
		c.BackoffFactor = 2.0
	}
	if c.PingInterval == 0 {
		c.PingInterval = 30 * time.Second
	}
	return c
}
