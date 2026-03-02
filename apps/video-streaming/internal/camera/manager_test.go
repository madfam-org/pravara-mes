package camera

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockDB is a mock database for testing
type MockDB struct {
	mock.Mock
}

func (m *MockDB) QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error) {
	arguments := m.Called(ctx, query, args)
	return arguments.Get(0).(*sql.Rows), arguments.Error(1)
}

func (m *MockDB) ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error) {
	arguments := m.Called(ctx, query, args)
	return arguments.Get(0).(sql.Result), arguments.Error(1)
}

func TestNewManager(t *testing.T) {
	// Create a mock database
	db := &sql.DB{}
	logger := logrus.New()

	manager := NewManager(db, logger)

	assert.NotNil(t, manager)
	assert.NotNil(t, manager.cameras)
	assert.NotNil(t, manager.streams)
}

func TestAddCamera(t *testing.T) {
	manager := &Manager{
		cameras: make(map[uuid.UUID]*Camera),
		streams: make(map[uuid.UUID]*Stream),
		log:     logrus.New(),
	}

	camera := &Camera{
		ID:        uuid.New(),
		Name:      "Test Camera",
		IPAddress: "192.168.1.100",
		StreamURL: "rtsp://192.168.1.100:554/stream",
		Status:    "online",
	}

	manager.AddCamera(camera)

	assert.Equal(t, 1, len(manager.cameras))
	assert.Equal(t, camera, manager.cameras[camera.ID])
}

func TestGetCamera(t *testing.T) {
	manager := &Manager{
		cameras: make(map[uuid.UUID]*Camera),
		log:     logrus.New(),
	}

	cameraID := uuid.New()
	camera := &Camera{
		ID:        cameraID,
		Name:      "Test Camera",
		IPAddress: "192.168.1.100",
	}

	manager.cameras[cameraID] = camera

	// Test getting existing camera
	result, exists := manager.GetCamera(cameraID)
	assert.True(t, exists)
	assert.Equal(t, camera, result)

	// Test getting non-existent camera
	nonExistentID := uuid.New()
	result, exists = manager.GetCamera(nonExistentID)
	assert.False(t, exists)
	assert.Nil(t, result)
}

func TestListCameras(t *testing.T) {
	manager := &Manager{
		cameras: make(map[uuid.UUID]*Camera),
		log:     logrus.New(),
	}

	// Add test cameras
	camera1 := &Camera{
		ID:   uuid.New(),
		Name: "Camera 1",
	}
	camera2 := &Camera{
		ID:   uuid.New(),
		Name: "Camera 2",
	}

	manager.cameras[camera1.ID] = camera1
	manager.cameras[camera2.ID] = camera2

	cameras := manager.ListCameras()

	assert.Equal(t, 2, len(cameras))
	// Check that both cameras are in the result
	found1, found2 := false, false
	for _, cam := range cameras {
		if cam.ID == camera1.ID {
			found1 = true
		}
		if cam.ID == camera2.ID {
			found2 = true
		}
	}
	assert.True(t, found1)
	assert.True(t, found2)
}

func TestStartStream(t *testing.T) {
	manager := &Manager{
		cameras: make(map[uuid.UUID]*Camera),
		streams: make(map[uuid.UUID]*Stream),
		log:     logrus.New(),
	}

	cameraID := uuid.New()
	camera := &Camera{
		ID:        cameraID,
		Name:      "Test Camera",
		StreamURL: "rtsp://192.168.1.100:554/stream",
	}

	manager.cameras[cameraID] = camera

	// Start stream
	streamID := manager.StartStream(cameraID, "high")

	assert.NotEqual(t, uuid.Nil, streamID)
	assert.Equal(t, 1, len(manager.streams))

	stream, exists := manager.streams[streamID]
	assert.True(t, exists)
	assert.Equal(t, cameraID, stream.CameraID)
	assert.Equal(t, "high", stream.Quality)
}

func TestStopStream(t *testing.T) {
	manager := &Manager{
		streams: make(map[uuid.UUID]*Stream),
		log:     logrus.New(),
	}

	streamID := uuid.New()
	stream := &Stream{
		ID:        streamID,
		CameraID:  uuid.New(),
		StartTime: time.Now(),
	}

	manager.streams[streamID] = stream

	// Stop stream
	manager.StopStream(streamID)

	assert.Equal(t, 0, len(manager.streams))
}

func TestMonitorCameraStatus(t *testing.T) {
	manager := &Manager{
		cameras: make(map[uuid.UUID]*Camera),
		log:     logrus.New(),
	}

	camera := &Camera{
		ID:        uuid.New(),
		Name:      "Test Camera",
		StreamURL: "rtsp://invalid.url/stream", // Use invalid URL for test
		Status:    "online",
	}

	manager.cameras[camera.ID] = camera

	// Update status (will fail due to invalid URL)
	manager.updateCameraStatus(camera.ID)

	// Status should change to offline
	assert.Equal(t, "offline", camera.Status)
}

func TestCameraCapabilities(t *testing.T) {
	capabilities := CameraCapabilities{
		PTZ:             true,
		Audio:           true,
		NightVision:     false,
		MotionDetection: true,
		Resolution:      []string{"1920x1080", "1280x720"},
		FPS:             []int{30, 25, 15},
	}

	assert.True(t, capabilities.PTZ)
	assert.True(t, capabilities.Audio)
	assert.False(t, capabilities.NightVision)
	assert.Equal(t, 2, len(capabilities.Resolution))
	assert.Equal(t, 3, len(capabilities.FPS))
}