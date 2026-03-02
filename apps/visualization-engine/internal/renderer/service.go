package renderer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
)

// Service handles 3D rendering coordination
type Service struct {
	log         *logrus.Logger
	clients     map[uuid.UUID]*Client
	clientsMu   sync.RWMutex
	sceneState  *SceneState
	stateMu     sync.RWMutex
}

// NewService creates a new renderer service
func NewService(log *logrus.Logger) *Service {
	return &Service{
		log:        log,
		clients:    make(map[uuid.UUID]*Client),
		sceneState: NewSceneState(),
	}
}

// Client represents a connected WebSocket client
type Client struct {
	ID       uuid.UUID
	Conn     *websocket.Conn
	TenantID uuid.UUID
	Send     chan []byte
	Hub      *Service
}

// SceneState represents the current state of the 3D scene
type SceneState struct {
	Machines  map[uuid.UUID]*MachineState `json:"machines"`
	Cameras   map[string]*CameraState      `json:"cameras"`
	Telemetry map[uuid.UUID]*TelemetryData `json:"telemetry"`
	UpdatedAt time.Time                    `json:"updated_at"`
}

// NewSceneState creates a new scene state
func NewSceneState() *SceneState {
	return &SceneState{
		Machines:  make(map[uuid.UUID]*MachineState),
		Cameras:   make(map[string]*CameraState),
		Telemetry: make(map[uuid.UUID]*TelemetryData),
		UpdatedAt: time.Now(),
	}
}

// MachineState represents a machine's state in the 3D scene
type MachineState struct {
	ID           uuid.UUID   `json:"id"`
	Position     Vector3     `json:"position"`
	Rotation     Vector3     `json:"rotation"`
	Scale        Vector3     `json:"scale"`
	Status       string      `json:"status"`
	ToolPosition Vector3     `json:"tool_position"`
	ModelURL     string      `json:"model_url"`
	Color        string      `json:"color"`
	Opacity      float64     `json:"opacity"`
	Visible      bool        `json:"visible"`
	Selected     bool        `json:"selected"`
	Animation    *Animation  `json:"animation,omitempty"`
}

// Vector3 represents a 3D vector
type Vector3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Animation represents an active animation
type Animation struct {
	Type     string  `json:"type"`     // "move", "rotate", "scale", "color"
	Target   Vector3 `json:"target"`   // Target position/rotation/scale
	Duration float64 `json:"duration"` // Seconds
	Easing   string  `json:"easing"`   // "linear", "ease-in-out", etc.
	Loop     bool    `json:"loop"`
	Progress float64 `json:"progress"` // 0-1
}

// CameraState represents a camera's state
type CameraState struct {
	Name     string  `json:"name"`
	Position Vector3 `json:"position"`
	Target   Vector3 `json:"target"`
	Up       Vector3 `json:"up"`
	FOV      float64 `json:"fov"`
	Near     float64 `json:"near"`
	Far      float64 `json:"far"`
	Type     string  `json:"type"` // "perspective", "orthographic"
}

// TelemetryData represents real-time telemetry
type TelemetryData struct {
	MachineID    uuid.UUID              `json:"machine_id"`
	Timestamp    time.Time              `json:"timestamp"`
	Position     Vector3                `json:"position"`
	FeedRate     float64                `json:"feed_rate"`
	SpindleSpeed float64                `json:"spindle_speed"`
	Temperature  float64                `json:"temperature"`
	Vibration    float64                `json:"vibration"`
	Power        float64                `json:"power"`
	Custom       map[string]interface{} `json:"custom"`
}

// Message types for WebSocket communication
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// SceneUpdate represents an update to the 3D scene
type SceneUpdate struct {
	Type      string      `json:"type"` // "machine", "camera", "telemetry", "full"
	MachineID *uuid.UUID  `json:"machine_id,omitempty"`
	Data      interface{} `json:"data"`
	Timestamp time.Time   `json:"timestamp"`
}

// HandleWebSocket handles a WebSocket connection
func (s *Service) HandleWebSocket(conn *websocket.Conn, rdb *redis.Client) {
	clientID := uuid.New()
	client := &Client{
		ID:   clientID,
		Conn: conn,
		Send: make(chan []byte, 256),
		Hub:  s,
	}

	// Register client
	s.clientsMu.Lock()
	s.clients[clientID] = client
	s.clientsMu.Unlock()

	// Send initial scene state
	s.sendSceneState(client)

	// Start goroutines for reading and writing
	go client.writePump()
	go client.readPump()

	// Subscribe to Redis updates
	go s.subscribeToUpdates(client, rdb)

	s.log.Infof("Client %s connected", clientID)
}

// sendSceneState sends the current scene state to a client
func (s *Service) sendSceneState(client *Client) {
	s.stateMu.RLock()
	state := s.sceneState
	s.stateMu.RUnlock()

	update := SceneUpdate{
		Type:      "full",
		Data:      state,
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		s.log.Errorf("Failed to marshal scene state: %v", err)
		return
	}

	select {
	case client.Send <- data:
	default:
		// Client's send channel is full
		s.removeClient(client)
	}
}

// subscribeToUpdates subscribes to Redis updates for a client
func (s *Service) subscribeToUpdates(client *Client, rdb *redis.Client) {
	ctx := context.Background()

	// Subscribe to telemetry and machine updates
	pubsub := rdb.Subscribe(ctx,
		fmt.Sprintf("telemetry:%s:*", client.TenantID),
		fmt.Sprintf("machines:%s:*", client.TenantID),
	)
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		// Parse and forward updates to client
		s.handleRedisMessage(client, msg)
	}
}

// handleRedisMessage handles a Redis pub/sub message
func (s *Service) handleRedisMessage(client *Client, msg *redis.Message) {
	// Parse message based on channel pattern
	if msg.Channel[:9] == "telemetry" {
		var telemetry TelemetryData
		if err := json.Unmarshal([]byte(msg.Payload), &telemetry); err != nil {
			s.log.Errorf("Failed to unmarshal telemetry: %v", err)
			return
		}

		// Update scene state
		s.stateMu.Lock()
		s.sceneState.Telemetry[telemetry.MachineID] = &telemetry

		// Update machine position if available
		if machine, ok := s.sceneState.Machines[telemetry.MachineID]; ok {
			machine.ToolPosition = telemetry.Position
		}
		s.stateMu.Unlock()

		// Send update to client
		update := SceneUpdate{
			Type:      "telemetry",
			MachineID: &telemetry.MachineID,
			Data:      telemetry,
			Timestamp: time.Now(),
		}

		s.broadcastUpdate(update)
	}
}

// UpdateMachinePosition updates a machine's position from telemetry
func (s *Service) UpdateMachinePosition(telemetryJSON string) {
	var telemetry TelemetryData
	if err := json.Unmarshal([]byte(telemetryJSON), &telemetry); err != nil {
		s.log.Errorf("Failed to unmarshal telemetry: %v", err)
		return
	}

	s.stateMu.Lock()
	s.sceneState.Telemetry[telemetry.MachineID] = &telemetry

	// Update machine tool position
	if machine, ok := s.sceneState.Machines[telemetry.MachineID]; ok {
		machine.ToolPosition = telemetry.Position

		// Animate the movement
		machine.Animation = &Animation{
			Type:     "move",
			Target:   telemetry.Position,
			Duration: 0.1, // 100ms smooth interpolation
			Easing:   "linear",
			Loop:     false,
			Progress: 0,
		}
	}
	s.sceneState.UpdatedAt = time.Now()
	s.stateMu.Unlock()

	// Broadcast update to all clients
	update := SceneUpdate{
		Type:      "telemetry",
		MachineID: &telemetry.MachineID,
		Data:      telemetry,
		Timestamp: time.Now(),
	}

	s.broadcastUpdate(update)
}

// broadcastUpdate sends an update to all connected clients
func (s *Service) broadcastUpdate(update SceneUpdate) {
	data, err := json.Marshal(update)
	if err != nil {
		s.log.Errorf("Failed to marshal update: %v", err)
		return
	}

	s.clientsMu.RLock()
	clients := make([]*Client, 0, len(s.clients))
	for _, client := range s.clients {
		clients = append(clients, client)
	}
	s.clientsMu.RUnlock()

	for _, client := range clients {
		select {
		case client.Send <- data:
		default:
			// Client's send channel is full
			s.removeClient(client)
		}
	}
}

// removeClient removes a client from the hub
func (s *Service) removeClient(client *Client) {
	s.clientsMu.Lock()
	if _, ok := s.clients[client.ID]; ok {
		delete(s.clients, client.ID)
		close(client.Send)
	}
	s.clientsMu.Unlock()
	s.log.Infof("Client %s disconnected", client.ID)
}

// Client pump methods

// readPump pumps messages from the WebSocket connection to the hub
func (c *Client) readPump() {
	defer func() {
		c.Hub.removeClient(c)
		c.Conn.Close()
	}()

	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		var msg Message
		err := c.Conn.ReadJSON(&msg)
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				c.Hub.log.Errorf("WebSocket error: %v", err)
			}
			break
		}

		// Handle client messages
		c.handleMessage(msg)
	}
}

// writePump pumps messages from the hub to the WebSocket connection
func (c *Client) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			c.Conn.WriteMessage(websocket.TextMessage, message)

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// handleMessage handles a message from the client
func (c *Client) handleMessage(msg Message) {
	switch msg.Type {
	case "camera_update":
		var camera CameraState
		if err := json.Unmarshal(msg.Payload, &camera); err != nil {
			c.Hub.log.Errorf("Failed to unmarshal camera update: %v", err)
			return
		}

		// Update camera state
		c.Hub.stateMu.Lock()
		c.Hub.sceneState.Cameras[camera.Name] = &camera
		c.Hub.stateMu.Unlock()

		// Broadcast to other clients
		update := SceneUpdate{
			Type:      "camera",
			Data:      camera,
			Timestamp: time.Now(),
		}
		c.Hub.broadcastUpdate(update)

	case "machine_select":
		var payload struct {
			MachineID uuid.UUID `json:"machine_id"`
			Selected  bool      `json:"selected"`
		}
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			c.Hub.log.Errorf("Failed to unmarshal machine select: %v", err)
			return
		}

		// Update machine selection
		c.Hub.stateMu.Lock()
		if machine, ok := c.Hub.sceneState.Machines[payload.MachineID]; ok {
			machine.Selected = payload.Selected
		}
		c.Hub.stateMu.Unlock()

	case "request_state":
		// Send current scene state
		c.Hub.sendSceneState(c)

	default:
		c.Hub.log.Warnf("Unknown message type: %s", msg.Type)
	}
}