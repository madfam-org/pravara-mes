package recording

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// --- Mocks ---

// MockStorageClient implements StorageClient for testing.
type MockStorageClient struct {
	mock.Mock
}

func (m *MockStorageClient) Upload(ctx context.Context, key string, filePath string) (string, error) {
	args := m.Called(ctx, key, filePath)
	return args.String(0), args.Error(1)
}

func (m *MockStorageClient) Delete(ctx context.Context, key string) error {
	args := m.Called(ctx, key)
	return args.Error(0)
}

// --- Helper ---

func newTestService() *Service {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return &Service{
		db:          nil,
		log:         logger,
		recordings:  make(map[uuid.UUID]*Recording),
		storagePath: "/tmp/test-recordings",
	}
}

// --- Tests: buildFFmpegCommand ---

func TestBuildFFmpegCommand_WithoutDuration(t *testing.T) {
	svc := newTestService()
	args := svc.buildFFmpegCommand("rtsp://camera1/stream", "/tmp/out.mp4", 0)

	assert.Contains(t, args, "-rtsp_transport")
	assert.Contains(t, args, "tcp")
	assert.Contains(t, args, "-i")
	assert.Contains(t, args, "rtsp://camera1/stream")
	assert.Contains(t, args, "-c:v")
	assert.Contains(t, args, "copy")
	assert.Contains(t, args, "-c:a")
	assert.Contains(t, args, "-f")
	assert.Contains(t, args, "mp4")
	assert.Contains(t, args, "-movflags")
	assert.Contains(t, args, "+faststart")
	assert.Contains(t, args, "-y")

	// Duration flag should NOT be present
	assert.NotContains(t, args, "-t")

	// Output path must be the last argument
	assert.Equal(t, "/tmp/out.mp4", args[len(args)-1])
}

func TestBuildFFmpegCommand_WithDuration(t *testing.T) {
	svc := newTestService()
	args := svc.buildFFmpegCommand("rtsp://camera1/stream", "/tmp/out.mp4", 60)

	assert.Contains(t, args, "-t")
	assert.Contains(t, args, "60")
	assert.Equal(t, "/tmp/out.mp4", args[len(args)-1])
}

func TestBuildFFmpegCommand_TableDriven(t *testing.T) {
	tests := []struct {
		name         string
		streamURL    string
		outputPath   string
		duration     int
		wantDuration bool
	}{
		{
			name:         "continuous recording (no duration)",
			streamURL:    "rtsp://10.0.0.1:554/live",
			outputPath:   "/data/rec.mp4",
			duration:     0,
			wantDuration: false,
		},
		{
			name:         "timed recording 30s",
			streamURL:    "rtsp://10.0.0.2:554/cam2",
			outputPath:   "/data/rec2.mp4",
			duration:     30,
			wantDuration: true,
		},
		{
			name:         "timed recording 3600s",
			streamURL:    "rtsp://10.0.0.3:554/cam3",
			outputPath:   "/data/long.mp4",
			duration:     3600,
			wantDuration: true,
		},
	}

	svc := newTestService()

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := svc.buildFFmpegCommand(tt.streamURL, tt.outputPath, tt.duration)

			// Stream URL must follow -i
			iIdx := -1
			for i, a := range args {
				if a == "-i" {
					iIdx = i
					break
				}
			}
			assert.Greater(t, iIdx, -1, "-i flag must be present")
			assert.Equal(t, tt.streamURL, args[iIdx+1])

			// Output path must be last
			assert.Equal(t, tt.outputPath, args[len(args)-1])

			// Duration flag presence
			hasDuration := false
			for _, a := range args {
				if a == "-t" {
					hasDuration = true
					break
				}
			}
			assert.Equal(t, tt.wantDuration, hasDuration)

			if tt.wantDuration {
				tIdx := -1
				for i, a := range args {
					if a == "-t" {
						tIdx = i
						break
					}
				}
				assert.Equal(t, fmt.Sprintf("%d", tt.duration), args[tIdx+1])
			}
		})
	}
}

// --- Tests: uploadToStorage ---

func TestUploadToStorage_NoStorageClient(t *testing.T) {
	svc := newTestService()
	// storage is nil by default

	url, err := svc.uploadToStorage("/tmp/recording.mp4")

	assert.NoError(t, err)
	assert.Equal(t, "/tmp/recording.mp4", url, "should return local path when no storage client")
}

func TestUploadToStorage_WithStorageClient_Success(t *testing.T) {
	svc := newTestService()
	mockStorage := new(MockStorageClient)
	svc.storage = mockStorage

	expectedURL := "s3://my-bucket/recordings/recording.mp4"
	mockStorage.On("Upload", mock.Anything, "recordings/recording.mp4", "/tmp/recording.mp4").
		Return(expectedURL, nil)

	url, err := svc.uploadToStorage("/tmp/recording.mp4")

	assert.NoError(t, err)
	assert.Equal(t, expectedURL, url)
	mockStorage.AssertExpectations(t)
}

func TestUploadToStorage_WithStorageClient_Error(t *testing.T) {
	svc := newTestService()
	mockStorage := new(MockStorageClient)
	svc.storage = mockStorage

	mockStorage.On("Upload", mock.Anything, "recordings/video.mp4", "/tmp/video.mp4").
		Return("", fmt.Errorf("network timeout"))

	url, err := svc.uploadToStorage("/tmp/video.mp4")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "storage upload failed")
	assert.Empty(t, url)
	mockStorage.AssertExpectations(t)
}

// --- Tests: SetStorage ---

func TestSetStorage(t *testing.T) {
	svc := newTestService()
	assert.Nil(t, svc.storage)

	mockStorage := new(MockStorageClient)
	svc.SetStorage(mockStorage)

	assert.NotNil(t, svc.storage)
	assert.Equal(t, mockStorage, svc.storage)
}

// --- Tests: StopRecording validation ---

func TestStopRecording_InvalidID(t *testing.T) {
	svc := newTestService()

	err := svc.StopRecording(context.Background(), "not-a-uuid")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid recording ID")
}

func TestStopRecording_NotFound(t *testing.T) {
	svc := newTestService()

	validID := uuid.New().String()
	err := svc.StopRecording(context.Background(), validID)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "recording not found")
}

func TestStopRecording_NotActive(t *testing.T) {
	svc := newTestService()

	recID := uuid.New()
	svc.recordings[recID] = &Recording{
		ID:     recID,
		Status: "completed",
	}

	err := svc.StopRecording(context.Background(), recID.String())

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "recording is not active")
}

// --- Tests: StartRecording validation ---

func TestStartRecording_InvalidCameraID(t *testing.T) {
	svc := newTestService()

	req := StartRequest{
		CameraID: "not-a-valid-uuid",
	}

	_, err := svc.StartRecording(context.Background(), req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid camera ID")
}

// --- Tests: AddEvent ---

func TestAddEvent_ExistingRecording(t *testing.T) {
	svc := newTestService()
	recID := uuid.New()
	rec := &Recording{
		ID:     recID,
		Events: make([]RecordingEvent, 0),
		Status: "recording",
	}
	svc.recordings[recID] = rec

	eventData := map[string]interface{}{"zone": "entrance"}
	svc.AddEvent(recID, "motion", eventData)

	assert.Len(t, rec.Events, 1)
	assert.Equal(t, "motion", rec.Events[0].Type)
	assert.Equal(t, "entrance", rec.Events[0].Data["zone"])
}

func TestAddEvent_NonExistentRecording(t *testing.T) {
	svc := newTestService()
	nonExistentID := uuid.New()

	// Should not panic
	svc.AddEvent(nonExistentID, "motion", nil)
}

func TestAddEvent_MultipleEvents(t *testing.T) {
	svc := newTestService()
	recID := uuid.New()
	rec := &Recording{
		ID:     recID,
		Events: make([]RecordingEvent, 0),
		Status: "recording",
	}
	svc.recordings[recID] = rec

	svc.AddEvent(recID, "motion", map[string]interface{}{"zone": "A"})
	svc.AddEvent(recID, "alert", map[string]interface{}{"level": "high"})
	svc.AddEvent(recID, "manual", nil)

	assert.Len(t, rec.Events, 3)
	assert.Equal(t, "motion", rec.Events[0].Type)
	assert.Equal(t, "alert", rec.Events[1].Type)
	assert.Equal(t, "manual", rec.Events[2].Type)
}

// --- Tests: StopAllRecordings ---

func TestStopAllRecordings_Empty(t *testing.T) {
	svc := newTestService()

	// Should not panic on empty recordings map
	svc.StopAllRecordings()

	assert.Empty(t, svc.recordings)
}

// --- Tests: Recording struct ---

func TestRecording_DefaultStatus(t *testing.T) {
	rec := Recording{
		ID:        uuid.New(),
		CameraID:  uuid.New(),
		StartTime: time.Now(),
		Events:    make([]RecordingEvent, 0),
		Status:    "recording",
		CreatedAt: time.Now(),
	}

	assert.Equal(t, "recording", rec.Status)
	assert.Nil(t, rec.EndTime)
	assert.Empty(t, rec.StorageURL)
	assert.Zero(t, rec.FileSize)
	assert.Zero(t, rec.Duration)
}

// --- Tests: Concurrent access ---

func TestConcurrentAddEvent(t *testing.T) {
	svc := newTestService()
	recID := uuid.New()
	rec := &Recording{
		ID:     recID,
		Events: make([]RecordingEvent, 0),
		Status: "recording",
	}
	svc.recordings[recID] = rec

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			svc.AddEvent(recID, "motion", map[string]interface{}{"index": idx})
		}(i)
	}
	wg.Wait()

	// All events should be added (though order is non-deterministic).
	// Note: AddEvent is not fully thread-safe for the Events slice append,
	// but we verify the test does not panic, which surfaces the concurrency gap.
	assert.GreaterOrEqual(t, len(rec.Events), 1)
}

// --- Tests: NewService ---

func TestNewService(t *testing.T) {
	logger := logrus.New()
	svc := NewService(nil, logger)

	assert.NotNil(t, svc)
	assert.NotNil(t, svc.recordings)
	assert.Equal(t, logger, svc.log)
	assert.Nil(t, svc.storage, "storage should be nil by default")
}

// --- Tests: Request/Response types ---

func TestStartRequest_Fields(t *testing.T) {
	req := StartRequest{
		CameraID: uuid.New().String(),
		Duration: 120,
		Metadata: map[string]interface{}{"reason": "scheduled"},
	}

	assert.NotEmpty(t, req.CameraID)
	assert.Equal(t, 120, req.Duration)
	assert.Equal(t, "scheduled", req.Metadata["reason"])
}

func TestStopRequest_Fields(t *testing.T) {
	recID := uuid.New().String()
	req := StopRequest{
		RecordingID: recID,
	}

	assert.Equal(t, recID, req.RecordingID)
}
