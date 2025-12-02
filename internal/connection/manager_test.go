package connection

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestManager_ConnectAndReceiveCommand(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_123",
					MetricsIntervalSeconds: 30,
					CommandsEnabled:        true,
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}

	manager := NewManager(Config{
		URL:          ms.URL(),
		Token:        "test",
		AgentVersion: "1.0.0",
		PingInterval: 1 * time.Second,
	})

	var stateChanges []State
	manager.OnStateChange(func(state State) {
		stateChanges = append(stateChanges, state)
	})

	var receivedCmd Command
	manager.OnCommand(func(cmd Command) {
		receivedCmd = cmd
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager.Start(ctx)

	// Wait for connection
	time.Sleep(200 * time.Millisecond)

	if manager.State() != StateConnected {
		t.Errorf("State() = %v, want %v", manager.State(), StateConnected)
	}

	// Send a command
	cmd := Message{
		Type: "command",
		Payload: map[string]interface{}{
			"id": "cmd_456",
			"signed_payload": map[string]interface{}{
				"server_id": "srv_123",
				"command":   "analyze",
				"nonce":     "xyz",
				"timestamp": float64(time.Now().UnixMilli()),
			},
			"signature": "sig123",
		},
	}
	ms.SendToAll(cmd)

	// Wait for command
	time.Sleep(200 * time.Millisecond)

	if receivedCmd.ID != "cmd_456" {
		t.Errorf("Command ID = %v, want %v", receivedCmd.ID, "cmd_456")
	}

	manager.Stop()

	// Verify state changes
	if len(stateChanges) < 2 {
		t.Errorf("expected at least 2 state changes, got %d", len(stateChanges))
	}
}

func TestManager_Reconnect(t *testing.T) {
	var connectionCount int32
	reconnected := make(chan struct{})

	ms := newMockServer(t)
	defer ms.Close()

	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			count := atomic.AddInt32(&connectionCount, 1)
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_123",
					MetricsIntervalSeconds: 30,
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)

			// Close connection after first connect to test reconnection
			if count == 1 {
				go func() {
					time.Sleep(50 * time.Millisecond)
					conn.Close()
				}()
			} else if count == 2 {
				// Signal that reconnection happened
				close(reconnected)
			}
		}
	}

	manager := NewManager(Config{
		URL:            ms.URL(),
		Token:          "test",
		AgentVersion:   "1.0.0",
		InitialBackoff: 50 * time.Millisecond,
		MaxBackoff:     200 * time.Millisecond,
		PingInterval:   5 * time.Second,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager.Start(ctx)

	// Wait for reconnection or timeout
	select {
	case <-reconnected:
		// Success - reconnection happened
	case <-time.After(3 * time.Second):
		count := atomic.LoadInt32(&connectionCount)
		t.Errorf("Timeout waiting for reconnect. Connection count: %d", count)
	}

	manager.Stop()
}

func TestManager_SendMetrics(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	var receivedMetrics map[string]interface{}
	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_123",
					MetricsIntervalSeconds: 1, // 1 second for fast test
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)
		}
		if msg.Type == "metrics" {
			if payload, ok := msg.Payload.(map[string]interface{}); ok {
				if metrics, ok := payload["metrics"].(map[string]interface{}); ok {
					receivedMetrics = metrics
				}
			}
		}
	}

	manager := NewManager(Config{
		URL:          ms.URL(),
		Token:        "test",
		AgentVersion: "1.0.0",
		PingInterval: 5 * time.Second,
	})

	manager.SetMetricsHandler(func() map[string]float64 {
		return map[string]float64{
			"test_metric": 42.0,
		}
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	manager.Start(ctx)

	// Wait for connection and metrics to be sent
	time.Sleep(1500 * time.Millisecond)

	if receivedMetrics == nil {
		t.Fatal("metrics were not received")
	}

	if receivedMetrics["test_metric"] != float64(42) {
		t.Errorf("test_metric = %v, want %v", receivedMetrics["test_metric"], 42)
	}

	manager.Stop()
}
