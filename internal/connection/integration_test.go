package connection

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// TestIntegration_AgentConnectsAndStaysConnected tests that the agent
// can establish a connection, stay connected, and receive/send messages.
func TestIntegration_AgentConnectsAndStaysConnected(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	var (
		pingCount    int32
		metricsCount int32
		mu           sync.Mutex
		lastMetrics  map[string]interface{}
	)

	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		switch msg.Type {
		case "agent_hello":
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_integration_test",
					MetricsIntervalSeconds: 1, // Fast metrics for testing
					CommandsEnabled:        true,
					SigningPublicKey:       "test-public-key",
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)

		case "ping":
			atomic.AddInt32(&pingCount, 1)
			pong := Message{
				Type:    "pong",
				Payload: msg.Payload,
			}
			data, _ := json.Marshal(pong)
			conn.WriteMessage(websocket.TextMessage, data)

		case "metrics":
			atomic.AddInt32(&metricsCount, 1)
			if payload, ok := msg.Payload.(map[string]interface{}); ok {
				if metrics, ok := payload["metrics"].(map[string]interface{}); ok {
					mu.Lock()
					lastMetrics = metrics
					mu.Unlock()
				}
			}
		}
	}

	manager := NewManager(Config{
		URL:            ms.URL(),
		Token:          "integration-test-token",
		AgentVersion:   "1.0.0",
		Hostname:       "test-host",
		OS:             "linux",
		Arch:           "amd64",
		PingInterval:   500 * time.Millisecond, // Fast pings for testing
		InitialBackoff: 100 * time.Millisecond,
		MaxBackoff:     1 * time.Second,
	})

	// Set up metrics handler
	manager.SetMetricsHandler(func() map[string]float64 {
		return map[string]float64{
			"pg_connections_active": 5,
			"pg_connections_idle":   10,
			"pg_database_size":      1024000,
		}
	})

	var stateChanges []State
	var statesMu sync.Mutex
	manager.OnStateChange(func(state State) {
		statesMu.Lock()
		stateChanges = append(stateChanges, state)
		statesMu.Unlock()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager.Start(ctx)

	// Wait for connection to be established
	time.Sleep(200 * time.Millisecond)

	// Verify connected state
	if manager.State() != StateConnected {
		t.Errorf("State() = %v, want %v", manager.State(), StateConnected)
	}

	// Verify server ID was received
	if manager.ServerID() != "srv_integration_test" {
		t.Errorf("ServerID() = %v, want %v", manager.ServerID(), "srv_integration_test")
	}

	// Verify signing key was received
	if manager.SigningPublicKey() != "test-public-key" {
		t.Errorf("SigningPublicKey() = %v, want %v", manager.SigningPublicKey(), "test-public-key")
	}

	// Wait long enough for at least one ping and one metrics send
	time.Sleep(1500 * time.Millisecond)

	// Verify pings were sent
	pings := atomic.LoadInt32(&pingCount)
	if pings < 1 {
		t.Errorf("pingCount = %d, want >= 1", pings)
	}

	// Verify metrics were sent
	metrics := atomic.LoadInt32(&metricsCount)
	if metrics < 1 {
		t.Errorf("metricsCount = %d, want >= 1", metrics)
	}

	// Verify metrics content
	mu.Lock()
	if lastMetrics == nil {
		t.Error("lastMetrics is nil")
	} else if lastMetrics["pg_connections_active"] != float64(5) {
		t.Errorf("pg_connections_active = %v, want %v", lastMetrics["pg_connections_active"], 5)
	}
	mu.Unlock()

	manager.Stop()

	// Verify state transitions
	statesMu.Lock()
	defer statesMu.Unlock()

	if len(stateChanges) < 2 {
		t.Errorf("expected at least 2 state changes, got %d", len(stateChanges))
	}

	// First state should be connecting
	if len(stateChanges) > 0 && stateChanges[0] != StateConnecting {
		t.Errorf("first state = %v, want %v", stateChanges[0], StateConnecting)
	}

	// Second state should be connected
	if len(stateChanges) > 1 && stateChanges[1] != StateConnected {
		t.Errorf("second state = %v, want %v", stateChanges[1], StateConnected)
	}
}

// TestIntegration_CommandExecution tests receiving and acknowledging commands.
func TestIntegration_CommandExecution(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	commandSent := make(chan struct{})
	resultReceived := make(chan CommandResultPayload, 1)

	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		switch msg.Type {
		case "agent_hello":
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_cmd_test",
					MetricsIntervalSeconds: 60,
					CommandsEnabled:        true,
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)

			// Send a command after connection
			go func() {
				time.Sleep(100 * time.Millisecond)
				cmd := Message{
					Type: "command",
					Payload: map[string]interface{}{
						"id": "cmd_exec_test",
						"signed_payload": map[string]interface{}{
							"server_id": "srv_cmd_test",
							"command":   "vacuum_analyze",
							"params": map[string]interface{}{
								"table": "users",
							},
							"nonce":     "test-nonce-123",
							"timestamp": float64(time.Now().UnixMilli()),
						},
						"signature": "test-signature",
					},
				}
				data, _ := json.Marshal(cmd)
				conn.WriteMessage(websocket.TextMessage, data)
				close(commandSent)
			}()

		case "command_result":
			payload, _ := json.Marshal(msg.Payload)
			var result CommandResultPayload
			json.Unmarshal(payload, &result)
			resultReceived <- result
		}
	}

	manager := NewManager(Config{
		URL:            ms.URL(),
		Token:          "cmd-test-token",
		AgentVersion:   "1.0.0",
		PingInterval:   10 * time.Second,
		InitialBackoff: 100 * time.Millisecond,
	})

	var receivedCmd Command
	cmdReceived := make(chan struct{})
	manager.OnCommand(func(cmd Command) {
		receivedCmd = cmd
		close(cmdReceived)

		// Simulate command execution and send result
		result := CommandResultPayload{
			CommandID:  cmd.ID,
			Status:     "success",
			DurationMs: 150,
			Result: map[string]interface{}{
				"rows_affected": 1000,
			},
		}
		manager.SendCommandResult(context.Background(), result)
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager.Start(ctx)

	// Wait for command to be received
	select {
	case <-cmdReceived:
		// Verify command details
		if receivedCmd.ID != "cmd_exec_test" {
			t.Errorf("Command.ID = %v, want %v", receivedCmd.ID, "cmd_exec_test")
		}
		if receivedCmd.Command != "vacuum_analyze" {
			t.Errorf("Command.Command = %v, want %v", receivedCmd.Command, "vacuum_analyze")
		}
		if receivedCmd.ServerID != "srv_cmd_test" {
			t.Errorf("Command.ServerID = %v, want %v", receivedCmd.ServerID, "srv_cmd_test")
		}
		if receivedCmd.Nonce != "test-nonce-123" {
			t.Errorf("Command.Nonce = %v, want %v", receivedCmd.Nonce, "test-nonce-123")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for command")
	}

	// Wait for result to be received by server
	select {
	case result := <-resultReceived:
		if result.CommandID != "cmd_exec_test" {
			t.Errorf("Result.CommandID = %v, want %v", result.CommandID, "cmd_exec_test")
		}
		if result.Status != "success" {
			t.Errorf("Result.Status = %v, want %v", result.Status, "success")
		}
		if result.DurationMs != 150 {
			t.Errorf("Result.DurationMs = %v, want %v", result.DurationMs, 150)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for command result")
	}

	manager.Stop()
}

// TestIntegration_GracefulShutdown tests that the agent shuts down cleanly.
func TestIntegration_GracefulShutdown(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_shutdown_test",
					MetricsIntervalSeconds: 60,
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}

	manager := NewManager(Config{
		URL:            ms.URL(),
		Token:          "shutdown-test-token",
		AgentVersion:   "1.0.0",
		PingInterval:   10 * time.Second,
		InitialBackoff: 100 * time.Millisecond,
	})

	ctx, cancel := context.WithCancel(context.Background())

	manager.Start(ctx)

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	if manager.State() != StateConnected {
		t.Errorf("State() = %v, want %v", manager.State(), StateConnected)
	}

	// Trigger shutdown via context cancellation
	cancel()

	// Give some time for shutdown to propagate
	time.Sleep(100 * time.Millisecond)

	// Also call Stop to ensure clean shutdown
	done := make(chan struct{})
	go func() {
		manager.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Shutdown completed successfully
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for graceful shutdown")
	}
}
