package recording

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// StorageClient defines the interface for cloud storage operations.
type StorageClient interface {
	Upload(ctx context.Context, key string, filePath string) (string, error)
	Delete(ctx context.Context, key string) error
}

// Service handles video recording
type Service struct {
	db           *sql.DB
	log          *logrus.Logger
	recordings   map[uuid.UUID]*Recording
	recordingsMu sync.RWMutex
	storagePath  string
	storage      StorageClient
}

// NewService creates a new recording service
func NewService(db *sql.DB, log *logrus.Logger) *Service {
	return &Service{
		db:          db,
		log:         log,
		recordings:  make(map[uuid.UUID]*Recording),
		storagePath: os.Getenv("RECORDING_STORAGE_PATH"),
	}
}

// SetStorage sets the cloud storage client for recording uploads.
func (s *Service) SetStorage(client StorageClient) {
	s.storage = client
}

// Recording represents a video recording
type Recording struct {
	ID           uuid.UUID              `json:"id"`
	CameraID     uuid.UUID              `json:"camera_id"`
	StartTime    time.Time              `json:"start_time"`
	EndTime      *time.Time             `json:"end_time,omitempty"`
	StorageURL   string                 `json:"storage_url"`
	FilePath     string                 `json:"file_path"`
	FileSize     int64                  `json:"file_size"`
	Duration     int                    `json:"duration_seconds"`
	Events       []RecordingEvent       `json:"events"`
	Metadata     map[string]interface{} `json:"metadata"`
	Status       string                 `json:"status"` // recording, completed, failed
	Process      *os.Process            `json:"-"`
	CreatedAt    time.Time              `json:"created_at"`
}

// RecordingEvent represents an event during recording
type RecordingEvent struct {
	Timestamp time.Time              `json:"timestamp"`
	Type      string                 `json:"type"` // motion, alert, manual
	Data      map[string]interface{} `json:"data"`
}

// StartRequest represents a recording start request
type StartRequest struct {
	CameraID string                 `json:"camera_id"`
	Duration int                    `json:"duration"` // 0 for continuous
	Metadata map[string]interface{} `json:"metadata"`
}

// StopRequest represents a recording stop request
type StopRequest struct {
	RecordingID string `json:"recording_id"`
}

// StartRecording starts a new recording
func (s *Service) StartRecording(ctx context.Context, req StartRequest) (string, error) {
	cameraID, err := uuid.Parse(req.CameraID)
	if err != nil {
		return "", fmt.Errorf("invalid camera ID: %w", err)
	}

	// Get camera details from database
	var streamURL string
	query := `SELECT stream_url FROM cameras WHERE id = $1`
	err = s.db.QueryRowContext(ctx, query, cameraID).Scan(&streamURL)
	if err != nil {
		return "", fmt.Errorf("failed to get camera: %w", err)
	}

	recordingID := uuid.New()
	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("recording_%s_%s.mp4", cameraID.String()[:8], timestamp)
	filePath := filepath.Join(s.storagePath, filename)

	// Create recording entry
	recording := &Recording{
		ID:        recordingID,
		CameraID:  cameraID,
		StartTime: time.Now(),
		FilePath:  filePath,
		Events:    make([]RecordingEvent, 0),
		Metadata:  req.Metadata,
		Status:    "recording",
		CreatedAt: time.Now(),
	}

	// Start FFmpeg process for recording
	ffmpegCmd := s.buildFFmpegCommand(streamURL, filePath, req.Duration)
	cmd := exec.CommandContext(ctx, "ffmpeg", ffmpegCmd...)

	// Set up logging
	cmd.Stdout = s.log.Writer()
	cmd.Stderr = s.log.Writer()

	// Start recording
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start recording: %w", err)
	}

	recording.Process = cmd.Process

	// Store recording in memory
	s.recordingsMu.Lock()
	s.recordings[recordingID] = recording
	s.recordingsMu.Unlock()

	// Store in database
	if err := s.saveRecording(ctx, recording); err != nil {
		// Try to stop the recording if database save fails
		cmd.Process.Kill()
		return "", fmt.Errorf("failed to save recording: %w", err)
	}

	// Monitor recording in background
	go s.monitorRecording(recording, cmd)

	// If duration is specified, set up auto-stop
	if req.Duration > 0 {
		go func() {
			time.Sleep(time.Duration(req.Duration) * time.Second)
			s.StopRecording(context.Background(), recordingID.String())
		}()
	}

	s.log.Infof("Started recording %s for camera %s", recordingID, cameraID)
	return recordingID.String(), nil
}

// StopRecording stops an active recording
func (s *Service) StopRecording(ctx context.Context, recordingID string) error {
	id, err := uuid.Parse(recordingID)
	if err != nil {
		return fmt.Errorf("invalid recording ID: %w", err)
	}

	s.recordingsMu.Lock()
	recording, ok := s.recordings[id]
	s.recordingsMu.Unlock()

	if !ok {
		return fmt.Errorf("recording not found")
	}

	if recording.Status != "recording" {
		return fmt.Errorf("recording is not active")
	}

	// Stop FFmpeg process
	if recording.Process != nil {
		// Send SIGINT for graceful shutdown
		if err := recording.Process.Signal(os.Interrupt); err != nil {
			// If SIGINT fails, use SIGKILL
			recording.Process.Kill()
		}

		// Wait for process to exit
		time.Sleep(2 * time.Second)
	}

	// Update recording status
	endTime := time.Now()
	recording.EndTime = &endTime
	recording.Status = "completed"
	recording.Duration = int(endTime.Sub(recording.StartTime).Seconds())

	// Get file size
	if fileInfo, err := os.Stat(recording.FilePath); err == nil {
		recording.FileSize = fileInfo.Size()
	}

	// Upload to storage (S3/R2)
	storageURL, err := s.uploadToStorage(recording.FilePath)
	if err != nil {
		s.log.Errorf("Failed to upload recording: %v", err)
		recording.Status = "failed"
	} else {
		recording.StorageURL = storageURL
	}

	// Update database
	if err := s.updateRecording(ctx, recording); err != nil {
		s.log.Errorf("Failed to update recording: %v", err)
	}

	// Remove from active recordings
	s.recordingsMu.Lock()
	delete(s.recordings, id)
	s.recordingsMu.Unlock()

	s.log.Infof("Stopped recording %s", recordingID)
	return nil
}

// StopAllRecordings stops all active recordings
func (s *Service) StopAllRecordings() {
	s.recordingsMu.RLock()
	ids := make([]uuid.UUID, 0, len(s.recordings))
	for id := range s.recordings {
		ids = append(ids, id)
	}
	s.recordingsMu.RUnlock()

	for _, id := range ids {
		if err := s.StopRecording(context.Background(), id.String()); err != nil {
			s.log.Errorf("Failed to stop recording %s: %v", id, err)
		}
	}
}

// ListRecordings lists all recordings
func (s *Service) ListRecordings(ctx context.Context) ([]Recording, error) {
	query := `
		SELECT id, camera_id, start_time, end_time, storage_url,
		       file_size, duration_seconds, events, metadata, created_at
		FROM video_recordings
		ORDER BY start_time DESC
		LIMIT 100
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query recordings: %w", err)
	}
	defer rows.Close()

	var recordings []Recording
	for rows.Next() {
		var rec Recording
		var endTime sql.NullTime
		var storageURL sql.NullString
		var events, metadata []byte

		err := rows.Scan(
			&rec.ID, &rec.CameraID, &rec.StartTime, &endTime, &storageURL,
			&rec.FileSize, &rec.Duration, &events, &metadata, &rec.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan recording: %w", err)
		}

		if endTime.Valid {
			rec.EndTime = &endTime.Time
			rec.Status = "completed"
		} else {
			rec.Status = "recording"
		}

		if storageURL.Valid {
			rec.StorageURL = storageURL.String
		}

		// Parse JSON fields
		json.Unmarshal(events, &rec.Events)
		json.Unmarshal(metadata, &rec.Metadata)

		recordings = append(recordings, rec)
	}

	return recordings, nil
}

// GetRecording retrieves a specific recording
func (s *Service) GetRecording(ctx context.Context, id string) (*Recording, error) {
	recordingID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid recording ID: %w", err)
	}

	query := `
		SELECT id, camera_id, start_time, end_time, storage_url,
		       file_size, duration_seconds, events, metadata, created_at
		FROM video_recordings
		WHERE id = $1
	`

	var rec Recording
	var endTime sql.NullTime
	var storageURL sql.NullString
	var events, metadata []byte

	err = s.db.QueryRowContext(ctx, query, recordingID).Scan(
		&rec.ID, &rec.CameraID, &rec.StartTime, &endTime, &storageURL,
		&rec.FileSize, &rec.Duration, &events, &metadata, &rec.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("recording not found")
		}
		return nil, fmt.Errorf("failed to query recording: %w", err)
	}

	if endTime.Valid {
		rec.EndTime = &endTime.Time
		rec.Status = "completed"
	} else {
		rec.Status = "recording"
	}

	if storageURL.Valid {
		rec.StorageURL = storageURL.String
	}

	// Parse JSON fields
	json.Unmarshal(events, &rec.Events)
	json.Unmarshal(metadata, &rec.Metadata)

	return &rec, nil
}

// DeleteRecording deletes a recording
func (s *Service) DeleteRecording(ctx context.Context, id string) error {
	recordingID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid recording ID: %w", err)
	}

	// Get recording details first
	rec, err := s.GetRecording(ctx, id)
	if err != nil {
		return err
	}

	// Delete from storage
	if rec.StorageURL != "" && s.storage != nil {
		key := fmt.Sprintf("recordings/%s", filepath.Base(rec.FilePath))
		if err := s.storage.Delete(context.Background(), key); err != nil {
			s.log.WithError(err).Error("Failed to delete from storage")
		}
	}

	// Delete local file if exists
	if rec.FilePath != "" {
		os.Remove(rec.FilePath)
	}

	// Delete from database
	query := `DELETE FROM video_recordings WHERE id = $1`
	result, err := s.db.ExecContext(ctx, query, recordingID)
	if err != nil {
		return fmt.Errorf("failed to delete recording: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("recording not found")
	}

	return nil
}

// AddEvent adds an event to a recording
func (s *Service) AddEvent(recordingID uuid.UUID, eventType string, data map[string]interface{}) {
	s.recordingsMu.RLock()
	recording, ok := s.recordings[recordingID]
	s.recordingsMu.RUnlock()

	if !ok {
		return
	}

	event := RecordingEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Data:      data,
	}

	recording.Events = append(recording.Events, event)

	// Update database
	ctx := context.Background()
	s.updateRecordingEvents(ctx, recordingID, recording.Events)
}

// Helper functions

// buildFFmpegCommand builds the FFmpeg command for recording
func (s *Service) buildFFmpegCommand(streamURL, outputPath string, duration int) []string {
	args := []string{
		"-rtsp_transport", "tcp",     // Use TCP for RTSP
		"-i", streamURL,               // Input stream
		"-c:v", "copy",                // Copy video codec (no re-encoding)
		"-c:a", "copy",                // Copy audio codec if present
		"-f", "mp4",                   // Output format
		"-movflags", "+faststart",     // Optimize for streaming
		"-y",                          // Overwrite output file
	}

	// Add duration if specified
	if duration > 0 {
		args = append(args, "-t", fmt.Sprintf("%d", duration))
	}

	args = append(args, outputPath)
	return args
}

// monitorRecording monitors the recording process
func (s *Service) monitorRecording(recording *Recording, cmd *exec.Cmd) {
	// Wait for process to complete
	err := cmd.Wait()

	if err != nil {
		s.log.Errorf("Recording process error: %v", err)
		recording.Status = "failed"
	} else {
		recording.Status = "completed"
	}

	// Update end time
	endTime := time.Now()
	recording.EndTime = &endTime
	recording.Duration = int(endTime.Sub(recording.StartTime).Seconds())

	// Get file size
	if fileInfo, err := os.Stat(recording.FilePath); err == nil {
		recording.FileSize = fileInfo.Size()
	}

	// Update database
	ctx := context.Background()
	s.updateRecording(ctx, recording)

	// Remove from active recordings
	s.recordingsMu.Lock()
	delete(s.recordings, recording.ID)
	s.recordingsMu.Unlock()
}

// saveRecording saves recording to database
func (s *Service) saveRecording(ctx context.Context, rec *Recording) error {
	events, _ := json.Marshal(rec.Events)
	metadata, _ := json.Marshal(rec.Metadata)

	query := `
		INSERT INTO video_recordings (
			id, camera_id, start_time, events, metadata, created_at
		) VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, err := s.db.ExecContext(ctx, query,
		rec.ID, rec.CameraID, rec.StartTime,
		events, metadata, rec.CreatedAt,
	)

	return err
}

// updateRecording updates recording in database
func (s *Service) updateRecording(ctx context.Context, rec *Recording) error {
	events, _ := json.Marshal(rec.Events)
	metadata, _ := json.Marshal(rec.Metadata)

	query := `
		UPDATE video_recordings SET
			end_time = $2, storage_url = $3, file_size = $4,
			duration_seconds = $5, events = $6, metadata = $7
		WHERE id = $1
	`

	var storageURL *string
	if rec.StorageURL != "" {
		storageURL = &rec.StorageURL
	}

	_, err := s.db.ExecContext(ctx, query,
		rec.ID, rec.EndTime, storageURL, rec.FileSize,
		rec.Duration, events, metadata,
	)

	return err
}

// updateRecordingEvents updates just the events field
func (s *Service) updateRecordingEvents(ctx context.Context, recordingID uuid.UUID, events []RecordingEvent) error {
	eventsJSON, _ := json.Marshal(events)

	query := `UPDATE video_recordings SET events = $2 WHERE id = $1`
	_, err := s.db.ExecContext(ctx, query, recordingID, eventsJSON)
	return err
}

// uploadToStorage uploads recording to cloud storage.
func (s *Service) uploadToStorage(filePath string) (string, error) {
	if s.storage == nil {
		// No storage configured, return local path
		return filePath, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	key := fmt.Sprintf("recordings/%s", filepath.Base(filePath))
	url, err := s.storage.Upload(ctx, key, filePath)
	if err != nil {
		return "", fmt.Errorf("storage upload failed: %w", err)
	}

	return url, nil
}