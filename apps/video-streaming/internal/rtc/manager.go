package rtc

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
	"github.com/pion/webrtc/v3/pkg/media"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/video-streaming/internal/camera"
)

// Manager handles WebRTC connections
type Manager struct {
	log         *logrus.Logger
	config      webrtc.Configuration
	peers       map[uuid.UUID]*PeerConnection
	peersMu     sync.RWMutex
	videoTracks map[string]*webrtc.TrackLocalStaticRTP
	tracksMu    sync.RWMutex
}

// NewManager creates a new RTC manager
func NewManager(log *logrus.Logger) *Manager {
	return &Manager{
		log:         log,
		peers:       make(map[uuid.UUID]*PeerConnection),
		videoTracks: make(map[string]*webrtc.TrackLocalStaticRTP),
	}
}

// SetConfig sets WebRTC configuration
func (m *Manager) SetConfig(config webrtc.Configuration) {
	m.config = config
}

// PeerConnection represents a WebRTC peer connection
type PeerConnection struct {
	ID         uuid.UUID
	PC         *webrtc.PeerConnection
	CameraID   string
	VideoTrack *webrtc.TrackLocalStaticRTP
	AudioTrack *webrtc.TrackLocalStaticRTP
	DataChan   *webrtc.DataChannel
	SignalChan chan interface{}
	CloseChan  chan bool
	CreatedAt  time.Time
}

// OfferRequest represents an offer request
type OfferRequest struct {
	SessionID string                     `json:"session_id"`
	CameraID  string                     `json:"camera_id"`
	Offer     webrtc.SessionDescription  `json:"offer"`
}

// AnswerRequest represents an answer request
type AnswerRequest struct {
	SessionID string                     `json:"session_id"`
	Answer    webrtc.SessionDescription  `json:"answer"`
}

// ICECandidateRequest represents an ICE candidate
type ICECandidateRequest struct {
	SessionID string                   `json:"session_id"`
	Candidate webrtc.ICECandidateInit `json:"candidate"`
}

// SignalingMessage represents a WebRTC signaling message
type SignalingMessage struct {
	Type      string          `json:"type"`
	SessionID string          `json:"session_id,omitempty"`
	CameraID  string          `json:"camera_id,omitempty"`
	Data      json.RawMessage `json:"data"`
}

// StreamInfo represents active stream information
type StreamInfo struct {
	SessionID string    `json:"session_id"`
	CameraID  string    `json:"camera_id"`
	State     string    `json:"state"`
	StartTime time.Time `json:"start_time"`
	Duration  string    `json:"duration"`
	Stats     PeerStats `json:"stats"`
}

// PeerStats represents peer connection statistics
type PeerStats struct {
	BytesSent     uint64  `json:"bytes_sent"`
	BytesReceived uint64  `json:"bytes_received"`
	PacketsLost   uint64  `json:"packets_lost"`
	Jitter        float64 `json:"jitter"`
	RTT           float64 `json:"rtt"`
}

// HandleOffer handles WebRTC offer
func (m *Manager) HandleOffer(ctx context.Context, req OfferRequest) (*webrtc.SessionDescription, error) {
	// Create new peer connection
	peerConnection, err := webrtc.NewPeerConnection(m.config)
	if err != nil {
		return nil, fmt.Errorf("failed to create peer connection: %w", err)
	}

	peerID := uuid.New()
	peer := &PeerConnection{
		ID:         peerID,
		PC:         peerConnection,
		CameraID:   req.CameraID,
		SignalChan: make(chan interface{}, 10),
		CloseChan:  make(chan bool),
		CreatedAt:  time.Now(),
	}

	// Add video track
	videoTrack, err := m.getOrCreateVideoTrack(req.CameraID)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to get video track: %w", err)
	}

	rtpSender, err := peerConnection.AddTrack(videoTrack)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to add video track: %w", err)
	}
	peer.VideoTrack = videoTrack

	// Read incoming RTCP packets (for sender reports)
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	// Create data channel for control messages
	dataChannel, err := peerConnection.CreateDataChannel("control", nil)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create data channel: %w", err)
	}
	peer.DataChan = dataChannel

	// Handle data channel events
	dataChannel.OnOpen(func() {
		m.log.Infof("Data channel opened for peer %s", peerID)
	})

	dataChannel.OnMessage(func(msg webrtc.DataChannelMessage) {
		m.handleDataChannelMessage(peer, msg)
	})

	// Set up ICE candidate handler
	peerConnection.OnICECandidate(func(candidate *webrtc.ICECandidate) {
		if candidate != nil {
			peer.SignalChan <- candidate.ToJSON()
		}
	})

	// Set up connection state handler
	peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		m.log.Infof("Peer %s connection state: %s", peerID, state.String())

		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed ||
			state == webrtc.PeerConnectionStateDisconnected {
			m.removePeer(peerID)
		}
	})

	// Set the remote description (offer)
	if err := peerConnection.SetRemoteDescription(req.Offer); err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to set remote description: %w", err)
	}

	// Create answer
	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to create answer: %w", err)
	}

	// Set local description
	if err := peerConnection.SetLocalDescription(answer); err != nil {
		peerConnection.Close()
		return nil, fmt.Errorf("failed to set local description: %w", err)
	}

	// Store peer connection
	m.peersMu.Lock()
	m.peers[peerID] = peer
	m.peersMu.Unlock()

	// Start streaming video to this peer
	go m.streamVideoToPeer(peer)

	return &answer, nil
}

// HandleAnswer handles WebRTC answer
func (m *Manager) HandleAnswer(ctx context.Context, req AnswerRequest) error {
	// Find peer by session ID
	peer := m.findPeerBySessionID(req.SessionID)
	if peer == nil {
		return fmt.Errorf("peer not found")
	}

	// Set remote description
	if err := peer.PC.SetRemoteDescription(req.Answer); err != nil {
		return fmt.Errorf("failed to set remote description: %w", err)
	}

	return nil
}

// HandleICECandidate handles ICE candidate
func (m *Manager) HandleICECandidate(ctx context.Context, req ICECandidateRequest) error {
	// Find peer by session ID
	peer := m.findPeerBySessionID(req.SessionID)
	if peer == nil {
		return fmt.Errorf("peer not found")
	}

	// Add ICE candidate
	if err := peer.PC.AddICECandidate(req.Candidate); err != nil {
		return fmt.Errorf("failed to add ICE candidate: %w", err)
	}

	return nil
}

// HandleSignalingWebSocket handles WebRTC signaling over WebSocket
func (m *Manager) HandleSignalingWebSocket(conn *websocket.Conn) {
	defer conn.Close()

	sessionID := uuid.New().String()
	m.log.Infof("WebSocket signaling session started: %s", sessionID)

	for {
		var msg SignalingMessage
		if err := conn.ReadJSON(&msg); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				m.log.Errorf("WebSocket error: %v", err)
			}
			break
		}

		switch msg.Type {
		case "offer":
			var offer webrtc.SessionDescription
			if err := json.Unmarshal(msg.Data, &offer); err != nil {
				m.sendError(conn, "Invalid offer format")
				continue
			}

			req := OfferRequest{
				SessionID: sessionID,
				CameraID:  msg.CameraID,
				Offer:     offer,
			}

			answer, err := m.HandleOffer(context.Background(), req)
			if err != nil {
				m.sendError(conn, err.Error())
				continue
			}

			// Send answer back
			response := SignalingMessage{
				Type:      "answer",
				SessionID: sessionID,
			}
			response.Data, _ = json.Marshal(answer)
			conn.WriteJSON(response)

		case "ice":
			var candidate webrtc.ICECandidateInit
			if err := json.Unmarshal(msg.Data, &candidate); err != nil {
				m.sendError(conn, "Invalid ICE candidate format")
				continue
			}

			req := ICECandidateRequest{
				SessionID: sessionID,
				Candidate: candidate,
			}

			if err := m.HandleICECandidate(context.Background(), req); err != nil {
				m.sendError(conn, err.Error())
			}

		case "close":
			m.closePeerBySessionID(sessionID)
			return

		default:
			m.sendError(conn, "Unknown message type")
		}
	}
}

// HandleCameraStream handles camera stream over WebSocket
func (m *Manager) HandleCameraStream(conn *websocket.Conn, cam *camera.Camera) {
	defer conn.Close()

	sessionID := uuid.New().String()
	m.log.Infof("Camera stream session started: %s for camera %s", sessionID, cam.ID)

	// Create peer connection for this camera stream
	peerConnection, err := webrtc.NewPeerConnection(m.config)
	if err != nil {
		m.log.Errorf("Failed to create peer connection: %v", err)
		return
	}
	defer peerConnection.Close()

	// Add tracks and handle signaling
	// This would integrate with the camera's actual stream source
	// For now, this is a placeholder
}

// getOrCreateVideoTrack gets or creates a video track for a camera
func (m *Manager) getOrCreateVideoTrack(cameraID string) (*webrtc.TrackLocalStaticRTP, error) {
	m.tracksMu.Lock()
	defer m.tracksMu.Unlock()

	// Check if track already exists
	if track, ok := m.videoTracks[cameraID]; ok {
		return track, nil
	}

	// Create new video track
	track, err := webrtc.NewTrackLocalStaticRTP(
		webrtc.RTPCodecCapability{MimeType: webrtc.MimeTypeH264},
		fmt.Sprintf("video-%s", cameraID),
		fmt.Sprintf("camera-%s", cameraID),
	)
	if err != nil {
		return nil, err
	}

	m.videoTracks[cameraID] = track
	return track, nil
}

// streamVideoToPeer streams video to a specific peer
func (m *Manager) streamVideoToPeer(peer *PeerConnection) {
	// This would connect to the actual camera stream and forward it
	// For demonstration, we'll generate a test pattern
	ticker := time.NewTicker(33 * time.Millisecond) // ~30 FPS
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// In production, this would read from the camera stream
			// and send actual video frames
			if peer.VideoTrack != nil {
				// Generate test frame (placeholder)
				frame := make([]byte, 1400) // MTU-sized packet
				for i := range frame {
					frame[i] = byte(i % 255)
				}

				// Write frame to track
				if err := peer.VideoTrack.WriteSample(media.Sample{
					Data:     frame,
					Duration: 33 * time.Millisecond,
				}); err != nil && err != io.ErrClosedPipe {
					m.log.Errorf("Failed to write video sample: %v", err)
				}
			}

		case <-peer.CloseChan:
			return
		}
	}
}

// handleDataChannelMessage handles messages from data channel
func (m *Manager) handleDataChannelMessage(peer *PeerConnection, msg webrtc.DataChannelMessage) {
	// Parse control message
	var control map[string]interface{}
	if err := json.Unmarshal(msg.Data, &control); err != nil {
		m.log.Errorf("Failed to parse control message: %v", err)
		return
	}

	// Handle different control messages
	if cmd, ok := control["command"].(string); ok {
		switch cmd {
		case "start_recording":
			m.log.Infof("Start recording requested for camera %s", peer.CameraID)
			// Trigger recording
		case "stop_recording":
			m.log.Infof("Stop recording requested for camera %s", peer.CameraID)
			// Stop recording
		case "ptz":
			// Handle PTZ control
			if params, ok := control["params"].(map[string]interface{}); ok {
				m.handlePTZControl(peer.CameraID, params)
			}
		}
	}
}

// handlePTZControl handles pan-tilt-zoom control
func (m *Manager) handlePTZControl(cameraID string, params map[string]interface{}) {
	// This would send PTZ commands to the actual camera
	m.log.Infof("PTZ control for camera %s: %+v", cameraID, params)
}

// findPeerBySessionID finds a peer by session ID
func (m *Manager) findPeerBySessionID(sessionID string) *PeerConnection {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()

	// In production, we'd maintain a session ID to peer ID mapping
	// For now, return the first matching peer (simplified)
	for _, peer := range m.peers {
		// This would check against actual session ID
		return peer
	}
	return nil
}

// closePeerBySessionID closes a peer connection by session ID
func (m *Manager) closePeerBySessionID(sessionID string) {
	peer := m.findPeerBySessionID(sessionID)
	if peer != nil {
		m.removePeer(peer.ID)
	}
}

// removePeer removes and closes a peer connection
func (m *Manager) removePeer(peerID uuid.UUID) {
	m.peersMu.Lock()
	defer m.peersMu.Unlock()

	if peer, ok := m.peers[peerID]; ok {
		close(peer.CloseChan)
		peer.PC.Close()
		delete(m.peers, peerID)
		m.log.Infof("Removed peer %s", peerID)
	}
}

// sendError sends an error message over WebSocket
func (m *Manager) sendError(conn *websocket.Conn, message string) {
	msg := SignalingMessage{
		Type: "error",
		Data: json.RawMessage(fmt.Sprintf(`{"error":"%s"}`, message)),
	}
	conn.WriteJSON(msg)
}

// ListActiveStreams lists all active streams
func (m *Manager) ListActiveStreams() []StreamInfo {
	m.peersMu.RLock()
	defer m.peersMu.RUnlock()

	var streams []StreamInfo
	for id, peer := range m.peers {
		info := StreamInfo{
			SessionID: id.String(),
			CameraID:  peer.CameraID,
			State:     peer.PC.ConnectionState().String(),
			StartTime: peer.CreatedAt,
			Duration:  time.Since(peer.CreatedAt).String(),
		}

		// Get stats
		stats := peer.PC.GetStats()
		for _, stat := range stats {
			// Parse relevant stats
			if transport, ok := stat.(webrtc.TransportStats); ok {
				info.Stats.BytesSent = transport.BytesSent
				info.Stats.BytesReceived = transport.BytesReceived
			}
		}

		streams = append(streams, info)
	}

	return streams
}

// MonitorStreams monitors active streams
func (m *Manager) MonitorStreams() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m.peersMu.RLock()
		peerCount := len(m.peers)
		m.peersMu.RUnlock()

		m.log.Infof("Active WebRTC peers: %d", peerCount)

		// Check for stale connections
		m.cleanupStaleConnections()
	}
}

// cleanupStaleConnections removes stale peer connections
func (m *Manager) cleanupStaleConnections() {
	m.peersMu.Lock()
	defer m.peersMu.Unlock()

	for id, peer := range m.peers {
		state := peer.PC.ConnectionState()
		if state == webrtc.PeerConnectionStateFailed ||
			state == webrtc.PeerConnectionStateClosed {
			peer.PC.Close()
			close(peer.CloseChan)
			delete(m.peers, id)
			m.log.Infof("Cleaned up stale peer %s", id)
		}
	}
}

// CloseAllConnections closes all peer connections
func (m *Manager) CloseAllConnections() {
	m.peersMu.Lock()
	defer m.peersMu.Unlock()

	for id, peer := range m.peers {
		peer.PC.Close()
		close(peer.CloseChan)
		delete(m.peers, id)
	}

	m.log.Info("Closed all peer connections")
}