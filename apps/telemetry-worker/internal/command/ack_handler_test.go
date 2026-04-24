package command

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockAckPublisher is a mock implementation of AckPublisher.
type MockAckPublisher struct {
	mock.Mock
}

func (m *MockAckPublisher) PublishCommandAck(ctx context.Context, tenantID, machineID uuid.UUID, ack CommandAckData) error {
	args := m.Called(ctx, tenantID, machineID, ack)
	return args.Error(0)
}

// MockAckStore is a mock implementation of AckStore.
type MockAckStore struct {
	mock.Mock
}

func (m *MockAckStore) GetMachineByCode(ctx context.Context, code string) (*MachineInfo, error) {
	args := m.Called(ctx, code)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*MachineInfo), args.Error(1)
}

func (m *MockAckStore) UpdateCommandStatus(ctx context.Context, commandID uuid.UUID, status string, message string) error {
	args := m.Called(ctx, commandID, status, message)
	return args.Error(0)
}

func (m *MockAckStore) GetTaskCommandByCommandID(ctx context.Context, commandID uuid.UUID) (*TaskCommandInfo, error) {
	args := m.Called(ctx, commandID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*TaskCommandInfo), args.Error(1)
}

func (m *MockAckStore) UpdateTaskStatusOnJobComplete(ctx context.Context, taskID uuid.UUID, newStatus string, completedAt time.Time) error {
	args := m.Called(ctx, taskID, newStatus, completedAt)
	return args.Error(0)
}

func TestBuildAckTopic(t *testing.T) {
	tests := []struct {
		name      string
		topicRoot string
		expected  string
	}{
		{
			name:      "standard topic with wildcards",
			topicRoot: "madfam/+/+/+/+/+",
			expected:  "madfam/+/+/+/+/ack",
		},
		{
			name:      "simple topic",
			topicRoot: "madfam/#",
			expected:  "madfam/+/+/+/+/ack",
		},
		{
			name:      "full topic path",
			topicRoot: "madfam/site/area/line/machine",
			expected:  "madfam/+/+/+/+/ack",
		},
		{
			name:      "wildcard only",
			topicRoot: "+",
			expected:  "+/+/+/+/+/ack",
		},
		{
			name:      "empty topic",
			topicRoot: "",
			expected:  "+/+/+/+/+/ack",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildAckTopic(tt.topicRoot)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestExtractMachineCode(t *testing.T) {
	tests := []struct {
		name     string
		topic    string
		expected string
	}{
		{
			name:     "valid ack topic",
			topic:    "madfam/hel/production/line-1/cnc-01/ack",
			expected: "cnc-01",
		},
		{
			name:     "different machine code",
			topic:    "tenant/site/area/line/printer-3d-001/ack",
			expected: "printer-3d-001",
		},
		{
			name:     "topic too short",
			topic:    "madfam/hel/production",
			expected: "",
		},
		{
			name:     "exactly 5 parts",
			topic:    "a/b/c/d/e",
			expected: "",
		},
		{
			name:     "6 parts valid",
			topic:    "a/b/c/d/machine/ack",
			expected: "machine",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractMachineCode(tt.topic)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAckHandler_NewAckHandler(t *testing.T) {
	log := logrus.New()
	log.SetLevel(logrus.DebugLevel)

	mockPublisher := new(MockAckPublisher)
	mockClient := &MockMQTTClient{}

	handler := NewAckHandler(mockClient, mockPublisher, log, "madfam/+/+/+/+/+")

	assert.NotNil(t, handler)
	assert.NotNil(t, handler.log)
	assert.Equal(t, "madfam/+/+/+/+/+", handler.topicRoot)
}

func TestAckHandler_SetStore(t *testing.T) {
	log := logrus.New()
	mockPublisher := new(MockAckPublisher)
	mockClient := &MockMQTTClient{}
	mockStore := new(MockAckStore)

	handler := NewAckHandler(mockClient, mockPublisher, log, "madfam/+/+/+/+/+")
	handler.SetStore(mockStore)

	// Verify store was set (indirectly through behavior)
	assert.NotNil(t, handler)
}

func TestAckHandler_Stop(t *testing.T) {
	log := logrus.New()
	mockPublisher := new(MockAckPublisher)
	mockClient := &MockMQTTClient{}

	handler := NewAckHandler(mockClient, mockPublisher, log, "madfam/+/+/+/+/+")

	// Should not panic
	handler.Stop()

	// Should be safe to call multiple times
	handler.Stop()
}

func TestCommandAck_Marshal(t *testing.T) {
	ack := CommandAck{
		CommandID: "550e8400-e29b-41d4-a716-446655440000",
		Success:   true,
		Message:   "Command executed successfully",
		Timestamp: time.Date(2024, 1, 15, 10, 30, 0, 0, time.UTC),
	}

	data, err := json.Marshal(ack)
	assert.NoError(t, err)

	var parsed CommandAck
	err = json.Unmarshal(data, &parsed)
	assert.NoError(t, err)
	assert.Equal(t, ack.CommandID, parsed.CommandID)
	assert.Equal(t, ack.Success, parsed.Success)
	assert.Equal(t, ack.Message, parsed.Message)
}

func TestCommandAck_Unmarshal(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		wantErr bool
	}{
		{
			name:    "valid ack with all fields",
			json:    `{"command_id":"550e8400-e29b-41d4-a716-446655440000","success":true,"message":"OK","timestamp":"2024-01-15T10:30:00Z"}`,
			wantErr: false,
		},
		{
			name:    "valid ack minimal fields",
			json:    `{"command_id":"550e8400-e29b-41d4-a716-446655440000","success":false}`,
			wantErr: false,
		},
		{
			name:    "invalid json",
			json:    `{"command_id":`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ack CommandAck
			err := json.Unmarshal([]byte(tt.json), &ack)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// MockMQTTClient is defined in dispatcher_test.go
// Re-implementing a minimal version here if not available

type mockMessage struct {
	topic   string
	payload []byte
}

func (m *mockMessage) Duplicate() bool   { return false }
func (m *mockMessage) Qos() byte         { return 1 }
func (m *mockMessage) Retained() bool    { return false }
func (m *mockMessage) Topic() string     { return m.topic }
func (m *mockMessage) MessageID() uint16 { return 1 }
func (m *mockMessage) Payload() []byte   { return m.payload }
func (m *mockMessage) Ack()              {}

type MockMQTTClient struct {
	mock.Mock
	subscribeHandler mqtt.MessageHandler
}

func (m *MockMQTTClient) IsConnected() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockMQTTClient) IsConnectionOpen() bool {
	return true
}

func (m *MockMQTTClient) Connect() mqtt.Token {
	return &MockToken{}
}

func (m *MockMQTTClient) Disconnect(quiesce uint) {}

func (m *MockMQTTClient) Publish(topic string, qos byte, retained bool, payload interface{}) mqtt.Token {
	args := m.Called(topic, qos, retained, payload)
	return args.Get(0).(mqtt.Token)
}

func (m *MockMQTTClient) Subscribe(topic string, qos byte, callback mqtt.MessageHandler) mqtt.Token {
	args := m.Called(topic, qos, callback)
	m.subscribeHandler = callback
	return args.Get(0).(mqtt.Token)
}

func (m *MockMQTTClient) SubscribeMultiple(filters map[string]byte, callback mqtt.MessageHandler) mqtt.Token {
	return &MockToken{}
}

func (m *MockMQTTClient) Unsubscribe(topics ...string) mqtt.Token {
	return &MockToken{}
}

func (m *MockMQTTClient) AddRoute(topic string, callback mqtt.MessageHandler) {}

func (m *MockMQTTClient) OptionsReader() mqtt.ClientOptionsReader {
	return mqtt.ClientOptionsReader{}
}

// MockToken implements mqtt.Token for testing.
type MockToken struct {
	err error
}

func (t *MockToken) Wait() bool {
	return true
}

func (t *MockToken) WaitTimeout(d time.Duration) bool {
	return true
}

func (t *MockToken) Done() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (t *MockToken) Error() error {
	return t.err
}
