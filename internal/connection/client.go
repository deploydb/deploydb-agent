package connection

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client manages the WebSocket connection to the control plane.
type Client struct {
	config   Config
	conn     *websocket.Conn
	mu       sync.RWMutex
	closed   bool
	closeCh  chan struct{}
	commands chan Command

	// State from welcome message
	serverID               string
	metricsIntervalSeconds int
	signingPublicKey       string
	commandsEnabled        bool

	// Logging
	logger *slog.Logger
}

// NewClient creates a new connection client.
func NewClient(config Config) *Client {
	config = config.WithDefaults()
	return &Client{
		config:   config,
		closeCh:  make(chan struct{}),
		commands: make(chan Command, 10),
		logger:   slog.Default(),
	}
}

// SetLogger sets the logger for the client.
func (c *Client) SetLogger(logger *slog.Logger) {
	c.logger = logger
}

// Connect establishes the WebSocket connection to the control plane.
func (c *Client) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	// Add token as query parameter
	url := c.config.URL
	if url[len(url)-1] != '?' {
		url += "?"
	}
	url += "token=" + c.config.Token

	c.logger.Debug("connecting to control plane", "url", c.config.URL)

	// Dial with context
	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	conn, resp, err := dialer.DialContext(ctx, url, http.Header{})
	if err != nil {
		if resp != nil {
			c.logger.Error("connection failed", "status", resp.StatusCode, "error", err)
		}
		return fmt.Errorf("dial: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()

	// Send agent_hello
	hello := Message{
		Type: "agent_hello",
		Payload: AgentHelloPayload{
			Token:           c.config.Token,
			AgentVersion:    c.config.AgentVersion,
			Hostname:        c.config.Hostname,
			OS:              c.config.OS,
			Arch:            c.config.Arch,
			PostgresVersion: c.config.PostgresVersion,
		},
	}

	if err := c.send(hello); err != nil {
		conn.Close()
		return fmt.Errorf("send agent_hello: %w", err)
	}

	c.logger.Debug("sent agent_hello")

	// Wait for welcome
	if err := c.waitForWelcome(ctx); err != nil {
		conn.Close()
		return fmt.Errorf("wait for welcome: %w", err)
	}

	c.logger.Info("connected to control plane",
		"server_id", c.serverID,
		"metrics_interval", c.metricsIntervalSeconds,
		"commands_enabled", c.commandsEnabled,
	)

	// Start message reader
	go c.readLoop()

	return nil
}

// waitForWelcome waits for the welcome message from the server.
func (c *Client) waitForWelcome(ctx context.Context) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	// Set read deadline based on context
	if deadline, ok := ctx.Deadline(); ok {
		conn.SetReadDeadline(deadline)
	}
	defer conn.SetReadDeadline(time.Time{})

	_, data, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read message: %w", err)
	}

	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return fmt.Errorf("unmarshal message: %w", err)
	}

	switch msg.Type {
	case "welcome":
		// Parse welcome payload
		payloadBytes, err := json.Marshal(msg.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}

		var welcome WelcomePayload
		if err := json.Unmarshal(payloadBytes, &welcome); err != nil {
			return fmt.Errorf("unmarshal welcome: %w", err)
		}

		c.mu.Lock()
		c.serverID = welcome.ServerID
		c.metricsIntervalSeconds = welcome.MetricsIntervalSeconds
		c.signingPublicKey = welcome.SigningPublicKey
		c.commandsEnabled = welcome.CommandsEnabled
		c.mu.Unlock()

		return nil

	case "error":
		payloadBytes, _ := json.Marshal(msg.Payload)
		var errPayload ErrorPayload
		json.Unmarshal(payloadBytes, &errPayload)
		return fmt.Errorf("server error: %s - %s", errPayload.Code, errPayload.Message)

	default:
		return fmt.Errorf("unexpected message type: %s", msg.Type)
	}
}

// readLoop reads messages from the WebSocket.
func (c *Client) readLoop() {
	defer func() {
		c.mu.Lock()
		if c.conn != nil {
			c.conn.Close()
			c.conn = nil // Signal disconnection
		}
		close(c.commands) // Close commands channel to signal readers
		c.mu.Unlock()
	}()

	for {
		select {
		case <-c.closeCh:
			return
		default:
		}

		c.mu.RLock()
		conn := c.conn
		closed := c.closed
		c.mu.RUnlock()

		if closed || conn == nil {
			return
		}

		_, data, err := conn.ReadMessage()
		if err != nil {
			if !c.isClosed() {
				c.logger.Error("read error", "error", err)
			}
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			c.logger.Warn("invalid message", "error", err)
			continue
		}

		c.handleMessage(msg)
	}
}

// handleMessage processes an incoming message.
func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case "command":
		c.handleCommand(msg)
	case "pong":
		// Pong received, connection is alive
		c.logger.Debug("pong received")
	case "error":
		payloadBytes, _ := json.Marshal(msg.Payload)
		var errPayload ErrorPayload
		json.Unmarshal(payloadBytes, &errPayload)
		c.logger.Error("server error", "code", errPayload.Code, "message", errPayload.Message)
	default:
		c.logger.Debug("unknown message type", "type", msg.Type)
	}
}

// handleCommand processes a command message.
func (c *Client) handleCommand(msg Message) {
	payloadBytes, err := json.Marshal(msg.Payload)
	if err != nil {
		c.logger.Error("marshal command payload", "error", err)
		return
	}

	var cmdPayload CommandPayload
	if err := json.Unmarshal(payloadBytes, &cmdPayload); err != nil {
		c.logger.Error("unmarshal command", "error", err)
		return
	}

	// Extract fields from signed_payload
	cmd := Command{
		ID:         cmdPayload.ID,
		Signature:  cmdPayload.Signature,
		RawPayload: cmdPayload.SignedPayload,
	}

	if serverID, ok := cmdPayload.SignedPayload["server_id"].(string); ok {
		cmd.ServerID = serverID
	}
	if command, ok := cmdPayload.SignedPayload["command"].(string); ok {
		cmd.Command = command
	}
	if params, ok := cmdPayload.SignedPayload["params"].(map[string]interface{}); ok {
		cmd.Params = params
	}
	if nonce, ok := cmdPayload.SignedPayload["nonce"].(string); ok {
		cmd.Nonce = nonce
	}
	if ts, ok := cmdPayload.SignedPayload["timestamp"].(float64); ok {
		cmd.Timestamp = time.UnixMilli(int64(ts))
	}

	select {
	case c.commands <- cmd:
		c.logger.Info("command received", "id", cmd.ID, "command", cmd.Command)
	default:
		c.logger.Warn("command channel full, dropping command", "id", cmd.ID)
	}
}

// send sends a message to the control plane.
func (c *Client) send(msg Message) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	c.mu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	c.mu.Unlock()

	return err
}

// Ping sends a ping message to keep the connection alive.
func (c *Client) Ping(ctx context.Context) error {
	msg := Message{
		Type: "ping",
		Payload: PingPayload{
			Timestamp: time.Now().UnixMilli(),
		},
	}
	return c.send(msg)
}

// SendMetrics sends metrics to the control plane.
func (c *Client) SendMetrics(ctx context.Context, metrics map[string]float64) error {
	msg := Message{
		Type: "metrics",
		Payload: MetricsPayload{
			Timestamp: time.Now().UnixMilli(),
			Metrics:   metrics,
		},
	}
	return c.send(msg)
}

// SendCommandResult sends the result of a command execution.
func (c *Client) SendCommandResult(ctx context.Context, result CommandResultPayload) error {
	msg := Message{
		Type:    "command_result",
		Payload: result,
	}
	return c.send(msg)
}

// Commands returns a channel of received commands.
func (c *Client) Commands() <-chan Command {
	return c.commands
}

// ServerID returns the server ID assigned by the control plane.
func (c *Client) ServerID() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.serverID
}

// MetricsInterval returns the metrics interval in seconds.
func (c *Client) MetricsInterval() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.metricsIntervalSeconds
}

// SigningPublicKey returns the command signing public key.
func (c *Client) SigningPublicKey() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.signingPublicKey
}

// CommandsEnabled returns whether command execution is enabled.
func (c *Client) CommandsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.commandsEnabled
}

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.closed {
		return nil
	}

	c.closed = true
	close(c.closeCh)

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// isClosed returns whether the client is closed.
func (c *Client) isClosed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.closed
}

// IsConnected returns whether the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && !c.closed
}
