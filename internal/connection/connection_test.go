package connection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// Mock WebSocket server for testing
type mockServer struct {
	server      *httptest.Server
	upgrader    websocket.Upgrader
	connections []*websocket.Conn
	mu          sync.Mutex
	onConnect   func(conn *websocket.Conn)
	onMessage   func(conn *websocket.Conn, msg Message)
}

func newMockServer(t *testing.T) *mockServer {
	ms := &mockServer{
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	ms.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := ms.upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}

		ms.mu.Lock()
		ms.connections = append(ms.connections, conn)
		ms.mu.Unlock()

		if ms.onConnect != nil {
			ms.onConnect(conn)
		}

		// Read messages
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				break
			}

			var msg Message
			if err := json.Unmarshal(data, &msg); err != nil {
				continue
			}

			if ms.onMessage != nil {
				ms.onMessage(conn, msg)
			}
		}
	}))

	return ms
}

func (ms *mockServer) URL() string {
	return "ws" + strings.TrimPrefix(ms.server.URL, "http")
}

func (ms *mockServer) Close() {
	ms.mu.Lock()
	for _, conn := range ms.connections {
		conn.Close()
	}
	ms.mu.Unlock()
	ms.server.Close()
}

func (ms *mockServer) SendToAll(msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	ms.mu.Lock()
	defer ms.mu.Unlock()

	for _, conn := range ms.connections {
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			return err
		}
	}
	return nil
}

func TestClient_Connect(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	var receivedHello *AgentHelloPayload
	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			payload, _ := json.Marshal(msg.Payload)
			var hello AgentHelloPayload
			json.Unmarshal(payload, &hello)
			receivedHello = &hello

			// Send welcome response
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

	client := NewClient(Config{
		URL:          ms.URL(),
		Token:        "ddb_testtoken123",
		AgentVersion: "1.0.0",
		Hostname:     "test-host",
		OS:           "linux",
		Arch:         "amd64",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// Wait for hello to be received
	time.Sleep(100 * time.Millisecond)

	if receivedHello == nil {
		t.Fatal("agent_hello was not received by server")
	}

	if receivedHello.Token != "ddb_testtoken123" {
		t.Errorf("Token = %v, want %v", receivedHello.Token, "ddb_testtoken123")
	}

	if receivedHello.AgentVersion != "1.0.0" {
		t.Errorf("AgentVersion = %v, want %v", receivedHello.AgentVersion, "1.0.0")
	}

	// Check welcome was processed
	if client.ServerID() != "srv_123" {
		t.Errorf("ServerID() = %v, want %v", client.ServerID(), "srv_123")
	}
}

func TestClient_ConnectFailsWithInvalidURL(t *testing.T) {
	client := NewClient(Config{
		URL:          "ws://invalid.localhost:99999",
		Token:        "test",
		AgentVersion: "1.0.0",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	err := client.Connect(ctx)
	if err == nil {
		t.Fatal("Connect() should have failed with invalid URL")
	}
}

func TestClient_Ping(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	var receivedPing bool
	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_123",
					MetricsIntervalSeconds: 30,
				},
			}
			data, _ := json.Marshal(welcome)
			conn.WriteMessage(websocket.TextMessage, data)
		}
		if msg.Type == "ping" {
			receivedPing = true
			// Send pong response
			pong := Message{
				Type:    "pong",
				Payload: msg.Payload,
			}
			data, _ := json.Marshal(pong)
			conn.WriteMessage(websocket.TextMessage, data)
		}
	}

	client := NewClient(Config{
		URL:          ms.URL(),
		Token:        "test",
		AgentVersion: "1.0.0",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// Send a ping
	err := client.Ping(ctx)
	if err != nil {
		t.Fatalf("Ping() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if !receivedPing {
		t.Error("ping was not received by server")
	}
}

func TestClient_SendMetrics(t *testing.T) {
	ms := newMockServer(t)
	defer ms.Close()

	var receivedMetrics map[string]interface{}
	ms.onMessage = func(conn *websocket.Conn, msg Message) {
		if msg.Type == "agent_hello" {
			welcome := Message{
				Type: "welcome",
				Payload: WelcomePayload{
					ServerID:               "srv_123",
					MetricsIntervalSeconds: 30,
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

	client := NewClient(Config{
		URL:          ms.URL(),
		Token:        "test",
		AgentVersion: "1.0.0",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// Send metrics
	metrics := map[string]float64{
		"pg_connections_active": 10,
		"pg_connections_idle":   5,
	}
	err := client.SendMetrics(ctx, metrics)
	if err != nil {
		t.Fatalf("SendMetrics() error = %v", err)
	}

	time.Sleep(100 * time.Millisecond)

	if receivedMetrics == nil {
		t.Fatal("metrics were not received by server")
	}

	if receivedMetrics["pg_connections_active"] != float64(10) {
		t.Errorf("pg_connections_active = %v, want %v", receivedMetrics["pg_connections_active"], 10)
	}
}

func TestClient_ReceiveCommand(t *testing.T) {
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

			// Send a command after a brief delay
			go func() {
				time.Sleep(50 * time.Millisecond)
				cmd := Message{
					Type: "command",
					Payload: map[string]interface{}{
						"id": "cmd_123",
						"signed_payload": map[string]interface{}{
							"server_id": "srv_123",
							"command":   "vacuum_analyze",
							"nonce":     "abc-123",
							"timestamp": time.Now().UnixMilli(),
						},
						"signature": "base64signature",
					},
				}
				data, _ := json.Marshal(cmd)
				conn.WriteMessage(websocket.TextMessage, data)
			}()
		}
	}

	client := NewClient(Config{
		URL:          ms.URL(),
		Token:        "test",
		AgentVersion: "1.0.0",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Connect(ctx); err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	defer client.Close()

	// Wait for command
	select {
	case cmd := <-client.Commands():
		if cmd.ID != "cmd_123" {
			t.Errorf("Command ID = %v, want %v", cmd.ID, "cmd_123")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for command")
	}
}

func TestBackoff(t *testing.T) {
	b := newBackoff(100*time.Millisecond, 1*time.Second, 2.0)

	// First attempt
	d1 := b.Next()
	if d1 != 100*time.Millisecond {
		t.Errorf("first backoff = %v, want %v", d1, 100*time.Millisecond)
	}

	// Second attempt (doubled)
	d2 := b.Next()
	if d2 != 200*time.Millisecond {
		t.Errorf("second backoff = %v, want %v", d2, 200*time.Millisecond)
	}

	// Third attempt
	d3 := b.Next()
	if d3 != 400*time.Millisecond {
		t.Errorf("third backoff = %v, want %v", d3, 400*time.Millisecond)
	}

	// Should cap at max
	for i := 0; i < 10; i++ {
		b.Next()
	}
	d := b.Next()
	if d != 1*time.Second {
		t.Errorf("capped backoff = %v, want %v", d, 1*time.Second)
	}

	// Reset should go back to initial
	b.Reset()
	d = b.Next()
	if d != 100*time.Millisecond {
		t.Errorf("reset backoff = %v, want %v", d, 100*time.Millisecond)
	}
}
