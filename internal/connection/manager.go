package connection

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// State represents the connection state.
type State int

const (
	StateDisconnected State = iota
	StateConnecting
	StateConnected
)

func (s State) String() string {
	switch s {
	case StateDisconnected:
		return "disconnected"
	case StateConnecting:
		return "connecting"
	case StateConnected:
		return "connected"
	default:
		return "unknown"
	}
}

// StateChangeHandler is called when connection state changes.
type StateChangeHandler func(state State)

// CommandHandler is called when a command is received.
type CommandHandler func(cmd Command)

// Manager manages the connection lifecycle including reconnection.
type Manager struct {
	config         Config
	client         *Client
	backoff        *backoff
	logger         *slog.Logger
	mu             sync.RWMutex
	state          State
	stopCh         chan struct{}
	stoppedCh      chan struct{}
	onStateChange  StateChangeHandler
	onCommand      CommandHandler
	pingTicker     *time.Ticker
	metricsHandler func() map[string]float64
}

// NewManager creates a new connection manager.
func NewManager(config Config) *Manager {
	config = config.WithDefaults()
	return &Manager{
		config:    config,
		backoff:   newBackoff(config.InitialBackoff, config.MaxBackoff, config.BackoffFactor),
		logger:    slog.Default(),
		state:     StateDisconnected,
		stopCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
	}
}

// SetLogger sets the logger.
func (m *Manager) SetLogger(logger *slog.Logger) {
	m.logger = logger
}

// OnStateChange sets the handler for state changes.
func (m *Manager) OnStateChange(handler StateChangeHandler) {
	m.onStateChange = handler
}

// OnCommand sets the handler for received commands.
func (m *Manager) OnCommand(handler CommandHandler) {
	m.onCommand = handler
}

// SetMetricsHandler sets the function that provides metrics to send.
func (m *Manager) SetMetricsHandler(handler func() map[string]float64) {
	m.metricsHandler = handler
}

// Start begins the connection manager loop.
// It will connect and automatically reconnect on failures.
func (m *Manager) Start(ctx context.Context) {
	go m.run(ctx)
}

// run is the main loop that manages connection and reconnection.
func (m *Manager) run(ctx context.Context) {
	defer close(m.stoppedCh)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("connection manager stopping (context cancelled)")
			m.cleanup()
			return
		case <-m.stopCh:
			m.logger.Info("connection manager stopping (stop requested)")
			m.cleanup()
			return
		default:
		}

		// Attempt to connect
		m.setState(StateConnecting)
		err := m.connect(ctx)
		if err != nil {
			m.logger.Error("connection failed", "error", err)
			m.setState(StateDisconnected)

			// Wait before retry
			backoffDuration := m.backoff.Next()
			m.logger.Info("reconnecting", "backoff", backoffDuration)

			select {
			case <-ctx.Done():
				return
			case <-m.stopCh:
				return
			case <-time.After(backoffDuration):
				continue
			}
		}

		// Connected successfully
		m.setState(StateConnected)
		m.backoff.Reset()

		// Run connected loop
		disconnectReason := m.runConnected(ctx)
		m.logger.Info("disconnected", "reason", disconnectReason)
		m.setState(StateDisconnected)

		// Brief pause before reconnecting
		select {
		case <-ctx.Done():
			return
		case <-m.stopCh:
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// connect attempts to establish a connection.
func (m *Manager) connect(ctx context.Context) error {
	// Create new client
	client := NewClient(m.config)
	client.SetLogger(m.logger)

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	if err := client.Connect(connectCtx); err != nil {
		return err
	}

	m.mu.Lock()
	m.client = client
	m.mu.Unlock()

	return nil
}

// runConnected handles the connected state: ping, metrics, commands.
func (m *Manager) runConnected(ctx context.Context) string {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil {
		return "no client"
	}

	// Start ping ticker
	pingTicker := time.NewTicker(m.config.PingInterval)
	defer pingTicker.Stop()

	// Start metrics ticker if handler is set
	var metricsTicker *time.Ticker
	if m.metricsHandler != nil && client.MetricsInterval() > 0 {
		metricsTicker = time.NewTicker(time.Duration(client.MetricsInterval()) * time.Second)
		defer metricsTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			client.Close()
			return "context cancelled"

		case <-m.stopCh:
			client.Close()
			return "stop requested"

		case <-pingTicker.C:
			if err := client.Ping(ctx); err != nil {
				m.logger.Error("ping failed", "error", err)
				client.Close()
				return "ping failed"
			}
			m.logger.Debug("ping sent")

		case cmd, ok := <-client.Commands():
			if !ok {
				return "commands channel closed"
			}
			if m.onCommand != nil {
				m.onCommand(cmd)
			}

		default:
			// Check if still connected
			if !client.IsConnected() {
				return "connection lost"
			}

			// Send metrics if ticker fired
			if metricsTicker != nil {
				select {
				case <-metricsTicker.C:
					if m.metricsHandler != nil {
						metrics := m.metricsHandler()
						if err := client.SendMetrics(ctx, metrics); err != nil {
							m.logger.Error("send metrics failed", "error", err)
						} else {
							m.logger.Debug("metrics sent", "count", len(metrics))
						}
					}
				default:
				}
			}

			// Small sleep to prevent tight loop
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// setState updates the connection state.
func (m *Manager) setState(state State) {
	m.mu.Lock()
	oldState := m.state
	m.state = state
	m.mu.Unlock()

	if oldState != state {
		m.logger.Info("state changed", "from", oldState, "to", state)
		if m.onStateChange != nil {
			m.onStateChange(state)
		}
	}
}

// State returns the current connection state.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// Client returns the current client (may be nil if disconnected).
func (m *Manager) Client() *Client {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.client
}

// Stop stops the connection manager.
func (m *Manager) Stop() {
	close(m.stopCh)
	<-m.stoppedCh
}

// cleanup closes the current client if any.
func (m *Manager) cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.client != nil {
		m.client.Close()
		m.client = nil
	}
	m.state = StateDisconnected
}

// SendCommandResult sends a command result through the current client.
func (m *Manager) SendCommandResult(ctx context.Context, result CommandResultPayload) error {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil {
		return ErrNotConnected
	}

	return client.SendCommandResult(ctx, result)
}

// ServerID returns the server ID from the current connection.
func (m *Manager) ServerID() string {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil {
		return ""
	}
	return client.ServerID()
}

// SigningPublicKey returns the signing public key from the current connection.
func (m *Manager) SigningPublicKey() string {
	m.mu.RLock()
	client := m.client
	m.mu.RUnlock()

	if client == nil {
		return ""
	}
	return client.SigningPublicKey()
}
