package renderer

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func newTestService() *Service {
	log := logrus.New()
	log.SetLevel(logrus.ErrorLevel)
	return NewService(log)
}

func addTestClient(s *Service) *Client {
	clientID := uuid.New()
	client := &Client{
		ID:   clientID,
		Send: make(chan []byte, 256),
		Hub:  s,
	}
	s.clientsMu.Lock()
	s.clients[clientID] = client
	s.clientsMu.Unlock()
	return client
}

func addMachineToScene(s *Service, machineID uuid.UUID) {
	s.stateMu.Lock()
	s.sceneState.Machines[machineID] = &MachineState{
		ID:       machineID,
		Position: Vector3{0, 0, 0},
		Scale:    Vector3{1, 1, 1},
		Status:   "idle",
		Visible:  true,
		Opacity:  1.0,
	}
	s.stateMu.Unlock()
}

// drainChannel reads all pending messages from a channel without blocking.
func drainChannel(ch <-chan []byte, timeout time.Duration) [][]byte {
	var msgs [][]byte
	deadline := time.After(timeout)
	for {
		select {
		case msg, ok := <-ch:
			if !ok {
				return msgs
			}
			msgs = append(msgs, msg)
		case <-deadline:
			return msgs
		}
	}
}

// ---------------------------------------------------------------------------
// NewService tests
// ---------------------------------------------------------------------------

func TestNewService(t *testing.T) {
	s := newTestService()
	if s == nil {
		t.Fatal("NewService returned nil")
	}
	if s.clients == nil {
		t.Error("clients map should be initialized")
	}
	if s.sceneState == nil {
		t.Error("sceneState should be initialized")
	}
}

func TestNewSceneState(t *testing.T) {
	ss := NewSceneState()
	if ss.Machines == nil {
		t.Error("Machines map should be initialized")
	}
	if ss.Cameras == nil {
		t.Error("Cameras map should be initialized")
	}
	if ss.Telemetry == nil {
		t.Error("Telemetry map should be initialized")
	}
	if ss.UpdatedAt.IsZero() {
		t.Error("UpdatedAt should be set")
	}
}

// ---------------------------------------------------------------------------
// Client registration and removal
// ---------------------------------------------------------------------------

func TestAddAndRemoveClient(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	s.clientsMu.RLock()
	if _, ok := s.clients[client.ID]; !ok {
		t.Error("client should be registered")
	}
	s.clientsMu.RUnlock()

	s.removeClient(client)

	s.clientsMu.RLock()
	if _, ok := s.clients[client.ID]; ok {
		t.Error("client should be removed after removeClient")
	}
	s.clientsMu.RUnlock()
}

func TestRemoveClient_ClosesChannel(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	s.removeClient(client)

	// Verify channel is closed
	_, ok := <-client.Send
	if ok {
		t.Error("Send channel should be closed after removal")
	}
}

func TestRemoveClient_Idempotent(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	// Removing twice should not panic
	s.removeClient(client)
	s.removeClient(client) // Should be a no-op
}

// ---------------------------------------------------------------------------
// UpdateMachinePosition tests
// ---------------------------------------------------------------------------

func TestUpdateMachinePosition_ValidPayload(t *testing.T) {
	s := newTestService()
	machineID := uuid.New()
	addMachineToScene(s, machineID)

	client := addTestClient(s)

	telemetry := TelemetryData{
		MachineID:    machineID,
		Timestamp:    time.Now(),
		Position:     Vector3{X: 100, Y: 200, Z: 50},
		FeedRate:     1500,
		SpindleSpeed: 12000,
		Temperature:  45.5,
	}
	payload, _ := json.Marshal(telemetry)

	s.UpdateMachinePosition(string(payload))

	// Verify scene state was updated
	s.stateMu.RLock()
	td, ok := s.sceneState.Telemetry[machineID]
	if !ok {
		t.Fatal("telemetry not stored in scene state")
	}
	if td.Position.X != 100 || td.Position.Y != 200 || td.Position.Z != 50 {
		t.Errorf("telemetry position = %v, want (100, 200, 50)", td.Position)
	}

	machine := s.sceneState.Machines[machineID]
	if machine.ToolPosition.X != 100 {
		t.Errorf("machine tool position X = %v, want 100", machine.ToolPosition.X)
	}
	if machine.Animation == nil {
		t.Error("machine should have animation set")
	}
	if machine.Animation != nil && machine.Animation.Type != "move" {
		t.Errorf("animation type = %q, want 'move'", machine.Animation.Type)
	}
	if machine.Animation != nil && machine.Animation.Duration != 0.1 {
		t.Errorf("animation duration = %v, want 0.1", machine.Animation.Duration)
	}
	s.stateMu.RUnlock()

	// Verify broadcast was sent to client
	msgs := drainChannel(client.Send, 100*time.Millisecond)
	if len(msgs) == 0 {
		t.Error("expected broadcast message to client")
	}

	// Parse the broadcast to verify structure
	if len(msgs) > 0 {
		var update SceneUpdate
		if err := json.Unmarshal(msgs[0], &update); err != nil {
			t.Fatalf("failed to unmarshal broadcast: %v", err)
		}
		if update.Type != "telemetry" {
			t.Errorf("update type = %q, want 'telemetry'", update.Type)
		}
		if update.MachineID == nil || *update.MachineID != machineID {
			t.Error("update machine ID mismatch")
		}
	}
}

func TestUpdateMachinePosition_InvalidJSON(t *testing.T) {
	s := newTestService()
	// Should not panic, just log error
	s.UpdateMachinePosition("not valid json{{{")
}

func TestUpdateMachinePosition_UnknownMachine(t *testing.T) {
	s := newTestService()
	unknownID := uuid.New()

	telemetry := TelemetryData{
		MachineID: unknownID,
		Position:  Vector3{X: 10, Y: 20, Z: 30},
	}
	payload, _ := json.Marshal(telemetry)

	// Should store telemetry but not update machine position (machine doesn't exist)
	s.UpdateMachinePosition(string(payload))

	s.stateMu.RLock()
	td, ok := s.sceneState.Telemetry[unknownID]
	s.stateMu.RUnlock()

	if !ok {
		t.Error("telemetry should still be stored even for unknown machine")
	}
	if td.Position.X != 10 {
		t.Errorf("stored position X = %v, want 10", td.Position.X)
	}
}

func TestUpdateMachinePosition_UpdatesTimestamp(t *testing.T) {
	s := newTestService()
	machineID := uuid.New()
	addMachineToScene(s, machineID)

	before := s.sceneState.UpdatedAt

	telemetry := TelemetryData{
		MachineID: machineID,
		Position:  Vector3{X: 1, Y: 2, Z: 3},
	}
	payload, _ := json.Marshal(telemetry)

	// Small delay so timestamp differs
	time.Sleep(1 * time.Millisecond)
	s.UpdateMachinePosition(string(payload))

	s.stateMu.RLock()
	after := s.sceneState.UpdatedAt
	s.stateMu.RUnlock()

	if !after.After(before) {
		t.Error("sceneState.UpdatedAt should be refreshed")
	}
}

// ---------------------------------------------------------------------------
// broadcastUpdate tests
// ---------------------------------------------------------------------------

func TestBroadcastUpdate_MultipleClients(t *testing.T) {
	s := newTestService()
	clients := make([]*Client, 5)
	for i := range clients {
		clients[i] = addTestClient(s)
	}

	update := SceneUpdate{
		Type:      "camera",
		Data:      CameraState{Name: "main", FOV: 75},
		Timestamp: time.Now(),
	}

	s.broadcastUpdate(update)

	for i, client := range clients {
		msgs := drainChannel(client.Send, 100*time.Millisecond)
		if len(msgs) != 1 {
			t.Errorf("client %d received %d messages, want 1", i, len(msgs))
		}
	}
}

func TestBroadcastUpdate_FullChannel_RemovesClient(t *testing.T) {
	s := newTestService()

	// Create client with tiny buffer that is already full
	clientID := uuid.New()
	client := &Client{
		ID:   clientID,
		Send: make(chan []byte, 1), // Buffer of 1
		Hub:  s,
	}
	s.clientsMu.Lock()
	s.clients[clientID] = client
	s.clientsMu.Unlock()

	// Fill the channel
	client.Send <- []byte("blocking")

	// This broadcast should fail to send and trigger removal
	update := SceneUpdate{
		Type:      "telemetry",
		Data:      "overflow",
		Timestamp: time.Now(),
	}
	s.broadcastUpdate(update)

	// Give removeClient a moment to execute
	time.Sleep(10 * time.Millisecond)

	s.clientsMu.RLock()
	_, exists := s.clients[clientID]
	s.clientsMu.RUnlock()

	if exists {
		t.Error("client with full channel should be removed")
	}
}

func TestBroadcastUpdate_NoClients(t *testing.T) {
	s := newTestService()
	// Should not panic with zero clients
	update := SceneUpdate{
		Type:      "full",
		Data:      s.sceneState,
		Timestamp: time.Now(),
	}
	s.broadcastUpdate(update)
}

// ---------------------------------------------------------------------------
// sendSceneState tests
// ---------------------------------------------------------------------------

func TestSendSceneState(t *testing.T) {
	s := newTestService()
	machineID := uuid.New()
	addMachineToScene(s, machineID)

	client := addTestClient(s)
	s.sendSceneState(client)

	msgs := drainChannel(client.Send, 100*time.Millisecond)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 scene state message, got %d", len(msgs))
	}

	var update SceneUpdate
	if err := json.Unmarshal(msgs[0], &update); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if update.Type != "full" {
		t.Errorf("update type = %q, want 'full'", update.Type)
	}
}

// ---------------------------------------------------------------------------
// handleMessage tests
// ---------------------------------------------------------------------------

func TestHandleMessage_CameraUpdate(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	camera := CameraState{
		Name:     "front",
		Position: Vector3{X: 0, Y: 10, Z: 50},
		Target:   Vector3{X: 0, Y: 0, Z: 0},
		FOV:      60,
		Type:     "perspective",
	}
	payload, _ := json.Marshal(camera)

	msg := Message{
		Type:    "camera_update",
		Payload: json.RawMessage(payload),
	}

	client.handleMessage(msg)

	s.stateMu.RLock()
	cam, ok := s.sceneState.Cameras["front"]
	s.stateMu.RUnlock()

	if !ok {
		t.Fatal("camera 'front' not stored in scene state")
	}
	if cam.FOV != 60 {
		t.Errorf("camera FOV = %v, want 60", cam.FOV)
	}
	if cam.Position.Y != 10 {
		t.Errorf("camera Y = %v, want 10", cam.Position.Y)
	}
}

func TestHandleMessage_MachineSelect(t *testing.T) {
	s := newTestService()
	machineID := uuid.New()
	addMachineToScene(s, machineID)

	client := addTestClient(s)

	selectPayload := struct {
		MachineID uuid.UUID `json:"machine_id"`
		Selected  bool      `json:"selected"`
	}{
		MachineID: machineID,
		Selected:  true,
	}
	payload, _ := json.Marshal(selectPayload)

	msg := Message{
		Type:    "machine_select",
		Payload: json.RawMessage(payload),
	}
	client.handleMessage(msg)

	s.stateMu.RLock()
	machine := s.sceneState.Machines[machineID]
	s.stateMu.RUnlock()

	if !machine.Selected {
		t.Error("machine should be selected after machine_select message")
	}
}

func TestHandleMessage_MachineSelect_UnknownMachine(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	selectPayload := struct {
		MachineID uuid.UUID `json:"machine_id"`
		Selected  bool      `json:"selected"`
	}{
		MachineID: uuid.New(),
		Selected:  true,
	}
	payload, _ := json.Marshal(selectPayload)

	msg := Message{
		Type:    "machine_select",
		Payload: json.RawMessage(payload),
	}

	// Should not panic for unknown machine
	client.handleMessage(msg)
}

func TestHandleMessage_RequestState(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	msg := Message{
		Type:    "request_state",
		Payload: json.RawMessage("{}"),
	}

	client.handleMessage(msg)

	msgs := drainChannel(client.Send, 100*time.Millisecond)
	if len(msgs) == 0 {
		t.Error("request_state should trigger scene state send")
	}
}

func TestHandleMessage_UnknownType(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	msg := Message{
		Type:    "nonexistent_type",
		Payload: json.RawMessage("{}"),
	}

	// Should not panic, just log a warning
	client.handleMessage(msg)
}

func TestHandleMessage_InvalidPayload(t *testing.T) {
	s := newTestService()
	client := addTestClient(s)

	msg := Message{
		Type:    "camera_update",
		Payload: json.RawMessage("not valid json"),
	}

	// Should not panic; error is logged
	client.handleMessage(msg)
}

// ---------------------------------------------------------------------------
// Concurrent client handling tests
// ---------------------------------------------------------------------------

func TestConcurrentClientRegistration(t *testing.T) {
	s := newTestService()
	var wg sync.WaitGroup
	clientCount := 50

	wg.Add(clientCount)
	for i := 0; i < clientCount; i++ {
		go func() {
			defer wg.Done()
			addTestClient(s)
		}()
	}
	wg.Wait()

	s.clientsMu.RLock()
	count := len(s.clients)
	s.clientsMu.RUnlock()

	if count != clientCount {
		t.Errorf("registered %d clients, want %d", count, clientCount)
	}
}

func TestConcurrentBroadcast(t *testing.T) {
	s := newTestService()
	clients := make([]*Client, 10)
	for i := range clients {
		clients[i] = addTestClient(s)
	}

	var wg sync.WaitGroup
	broadcastCount := 20

	wg.Add(broadcastCount)
	for i := 0; i < broadcastCount; i++ {
		go func(idx int) {
			defer wg.Done()
			update := SceneUpdate{
				Type:      "telemetry",
				Data:      map[string]int{"seq": idx},
				Timestamp: time.Now(),
			}
			s.broadcastUpdate(update)
		}(i)
	}
	wg.Wait()

	// Each client should have received some messages
	for i, client := range clients {
		msgs := drainChannel(client.Send, 200*time.Millisecond)
		if len(msgs) == 0 {
			t.Errorf("client %d received 0 messages, expected some", i)
		}
	}
}

func TestConcurrentUpdateAndBroadcast(t *testing.T) {
	s := newTestService()
	machineID := uuid.New()
	addMachineToScene(s, machineID)

	client := addTestClient(s)

	var wg sync.WaitGroup
	updateCount := 30

	wg.Add(updateCount)
	for i := 0; i < updateCount; i++ {
		go func(idx int) {
			defer wg.Done()
			telemetry := TelemetryData{
				MachineID: machineID,
				Position:  Vector3{X: float64(idx), Y: float64(idx), Z: 0},
			}
			payload, _ := json.Marshal(telemetry)
			s.UpdateMachinePosition(string(payload))
		}(i)
	}
	wg.Wait()

	msgs := drainChannel(client.Send, 200*time.Millisecond)
	if len(msgs) == 0 {
		t.Error("expected broadcast messages from concurrent updates")
	}

	// Verify scene state is consistent (no data races detected by -race flag)
	s.stateMu.RLock()
	_, ok := s.sceneState.Telemetry[machineID]
	s.stateMu.RUnlock()
	if !ok {
		t.Error("telemetry should exist after updates")
	}
}

func TestConcurrentAddRemoveClients(t *testing.T) {
	s := newTestService()
	var wg sync.WaitGroup

	// Concurrently add and remove clients
	wg.Add(40)
	for i := 0; i < 20; i++ {
		go func() {
			defer wg.Done()
			c := addTestClient(s)
			// Small delay then remove
			time.Sleep(time.Millisecond)
			s.removeClient(c)
		}()
		go func() {
			defer wg.Done()
			addTestClient(s)
		}()
	}
	wg.Wait()

	// Should not panic or deadlock; remaining client count is non-deterministic
	s.clientsMu.RLock()
	count := len(s.clients)
	s.clientsMu.RUnlock()
	t.Logf("remaining clients after concurrent add/remove: %d", count)
}

// ---------------------------------------------------------------------------
// Data type tests
// ---------------------------------------------------------------------------

func TestSceneUpdate_JSONMarshal(t *testing.T) {
	machineID := uuid.New()
	update := SceneUpdate{
		Type:      "telemetry",
		MachineID: &machineID,
		Data: TelemetryData{
			MachineID:    machineID,
			Position:     Vector3{X: 1, Y: 2, Z: 3},
			FeedRate:     1000,
			SpindleSpeed: 5000,
		},
		Timestamp: time.Now(),
	}

	data, err := json.Marshal(update)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded SceneUpdate
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Type != "telemetry" {
		t.Errorf("type = %q, want 'telemetry'", decoded.Type)
	}
	if decoded.MachineID == nil || *decoded.MachineID != machineID {
		t.Error("machine ID mismatch in round trip")
	}
}

func TestMachineState_Defaults(t *testing.T) {
	ms := MachineState{
		ID:      uuid.New(),
		Status:  "running",
		Visible: true,
		Opacity: 1.0,
	}

	data, err := json.Marshal(ms)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded MachineState
	json.Unmarshal(data, &decoded)
	if decoded.Status != "running" {
		t.Errorf("status = %q, want 'running'", decoded.Status)
	}
	if !decoded.Visible {
		t.Error("visible should be true")
	}
	if decoded.Animation != nil {
		t.Error("animation should be nil (omitempty)")
	}
}
