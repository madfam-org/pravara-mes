package rtc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Helper ---

func newTestManager() *Manager {
	logger := logrus.New()
	logger.SetLevel(logrus.ErrorLevel)
	return NewManager(logger)
}

// newTestManagerWithSTUN creates a manager configured with the public Google STUN server
// so that peer connections can gather ICE candidates in test environments.
func newTestManagerWithSTUN() *Manager {
	m := newTestManager()
	m.SetConfig(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	return m
}

// --- Tests: NewManager ---

func TestNewManager(t *testing.T) {
	m := newTestManager()

	assert.NotNil(t, m)
	assert.NotNil(t, m.peers)
	assert.NotNil(t, m.videoTracks)
	assert.NotNil(t, m.log)
}

// --- Tests: SetConfig ---

func TestSetConfig(t *testing.T) {
	m := newTestManager()

	cfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	}

	m.SetConfig(cfg)

	assert.Len(t, m.config.ICEServers, 1)
	assert.Equal(t, "stun:stun.l.google.com:19302", m.config.ICEServers[0].URLs[0])
}

// --- Tests: getOrCreateVideoTrack ---

func TestGetOrCreateVideoTrack_CreatesNew(t *testing.T) {
	m := newTestManager()

	track, err := m.getOrCreateVideoTrack("camera-001")

	assert.NoError(t, err)
	assert.NotNil(t, track)
	assert.Equal(t, "video-camera-001", track.ID())
	assert.Equal(t, "camera-camera-001", track.StreamID())
}

func TestGetOrCreateVideoTrack_ReturnsCached(t *testing.T) {
	m := newTestManager()

	track1, err := m.getOrCreateVideoTrack("camera-002")
	require.NoError(t, err)

	track2, err := m.getOrCreateVideoTrack("camera-002")
	require.NoError(t, err)

	// Should return the exact same track pointer
	assert.Same(t, track1, track2)
	assert.Len(t, m.videoTracks, 1)
}

func TestGetOrCreateVideoTrack_MultipleCameras(t *testing.T) {
	m := newTestManager()

	track1, err := m.getOrCreateVideoTrack("cam-A")
	require.NoError(t, err)

	track2, err := m.getOrCreateVideoTrack("cam-B")
	require.NoError(t, err)

	assert.NotSame(t, track1, track2)
	assert.Len(t, m.videoTracks, 2)
}

// --- Tests: HandleOffer flow ---

func TestHandleOffer_ValidOffer(t *testing.T) {
	m := newTestManagerWithSTUN()

	// Create a client-side peer connection to generate a real offer
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()

	// Add a transceiver to receive video
	_, err = clientPC.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	require.NoError(t, err)

	offer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)

	err = clientPC.SetLocalDescription(offer)
	require.NoError(t, err)

	req := OfferRequest{
		SessionID: uuid.New().String(),
		CameraID:  "test-camera",
		Offer:     offer,
	}

	answer, err := m.HandleOffer(t.Context(), req)

	assert.NoError(t, err)
	assert.NotNil(t, answer)
	assert.Equal(t, webrtc.SDPTypeAnswer, answer.Type)
	assert.NotEmpty(t, answer.SDP)

	// Verify peer was stored
	m.peersMu.RLock()
	peerCount := len(m.peers)
	m.peersMu.RUnlock()
	assert.Equal(t, 1, peerCount)

	// Cleanup
	m.CloseAllConnections()
}

// --- Tests: HandleAnswer ---

func TestHandleAnswer_PeerNotFound(t *testing.T) {
	m := newTestManager()

	req := AnswerRequest{
		SessionID: "nonexistent-session",
		Answer: webrtc.SessionDescription{
			Type: webrtc.SDPTypeAnswer,
			SDP:  "invalid",
		},
	}

	err := m.HandleAnswer(t.Context(), req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "peer not found")
}

// --- Tests: HandleICECandidate ---

func TestHandleICECandidate_PeerNotFound(t *testing.T) {
	m := newTestManager()

	candidateStr := "candidate:1 1 UDP 2130706431 192.168.1.1 5000 typ host"
	req := ICECandidateRequest{
		SessionID: "nonexistent-session",
		Candidate: webrtc.ICECandidateInit{
			Candidate: candidateStr,
		},
	}

	err := m.HandleICECandidate(t.Context(), req)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "peer not found")
}

func TestHandleICECandidate_WithActivePeer(t *testing.T) {
	m := newTestManagerWithSTUN()

	// Set up a real peer connection via HandleOffer
	clientPC, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer clientPC.Close()

	_, err = clientPC.AddTransceiverFromKind(webrtc.RTPCodecTypeVideo, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionRecvonly,
	})
	require.NoError(t, err)

	offer, err := clientPC.CreateOffer(nil)
	require.NoError(t, err)
	err = clientPC.SetLocalDescription(offer)
	require.NoError(t, err)

	sessionID := uuid.New().String()
	offerReq := OfferRequest{
		SessionID: sessionID,
		CameraID:  "test-camera",
		Offer:     offer,
	}

	answer, err := m.HandleOffer(t.Context(), offerReq)
	require.NoError(t, err)

	// Set the answer on the client side so both sides have descriptions
	err = clientPC.SetRemoteDescription(*answer)
	require.NoError(t, err)

	// Now try adding a valid ICE candidate
	// Using an empty candidate string is valid for end-of-candidates signaling
	candidateReq := ICECandidateRequest{
		SessionID: sessionID,
		Candidate: webrtc.ICECandidateInit{
			Candidate: "",
		},
	}

	// With findPeerBySessionID returning the first peer, this should find our peer
	err = m.HandleICECandidate(t.Context(), candidateReq)
	// The empty candidate may succeed or fail depending on pion version;
	// the key assertion is that we found the peer and attempted the operation
	// (no "peer not found" error).
	if err != nil {
		assert.NotContains(t, err.Error(), "peer not found")
	}

	m.CloseAllConnections()
}

// --- Tests: removePeer ---

func TestRemovePeer_ExistingPeer(t *testing.T) {
	m := newTestManager()

	// Create a real peer connection
	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)

	peerID := uuid.New()
	peer := &PeerConnection{
		ID:         peerID,
		PC:         pc,
		CameraID:   "cam-1",
		SignalChan: make(chan interface{}, 10),
		CloseChan:  make(chan bool),
		CreatedAt:  time.Now(),
	}

	m.peersMu.Lock()
	m.peers[peerID] = peer
	m.peersMu.Unlock()

	m.removePeer(peerID)

	m.peersMu.RLock()
	_, exists := m.peers[peerID]
	m.peersMu.RUnlock()

	assert.False(t, exists)
}

func TestRemovePeer_NonExistent(t *testing.T) {
	m := newTestManager()

	// Should not panic
	m.removePeer(uuid.New())

	assert.Empty(t, m.peers)
}

// --- Tests: ListActiveStreams ---

func TestListActiveStreams_Empty(t *testing.T) {
	m := newTestManager()

	streams := m.ListActiveStreams()

	assert.Empty(t, streams)
}

func TestListActiveStreams_WithPeers(t *testing.T) {
	m := newTestManager()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer pc.Close()

	peerID := uuid.New()
	peer := &PeerConnection{
		ID:        peerID,
		PC:        pc,
		CameraID:  "camera-42",
		CloseChan: make(chan bool),
		CreatedAt: time.Now().Add(-5 * time.Minute),
	}

	m.peersMu.Lock()
	m.peers[peerID] = peer
	m.peersMu.Unlock()

	streams := m.ListActiveStreams()

	assert.Len(t, streams, 1)
	assert.Equal(t, peerID.String(), streams[0].SessionID)
	assert.Equal(t, "camera-42", streams[0].CameraID)
}

// --- Tests: CloseAllConnections ---

func TestCloseAllConnections(t *testing.T) {
	m := newTestManager()

	// Add multiple peers
	for i := 0; i < 3; i++ {
		pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
		require.NoError(t, err)

		peerID := uuid.New()
		m.peers[peerID] = &PeerConnection{
			ID:        peerID,
			PC:        pc,
			CloseChan: make(chan bool),
			CreatedAt: time.Now(),
		}
	}

	assert.Len(t, m.peers, 3)

	m.CloseAllConnections()

	assert.Empty(t, m.peers)
}

// --- Tests: findPeerBySessionID ---

func TestFindPeerBySessionID_NoPeers(t *testing.T) {
	m := newTestManager()

	peer := m.findPeerBySessionID("any-session")

	assert.Nil(t, peer)
}

func TestFindPeerBySessionID_WithPeer(t *testing.T) {
	m := newTestManager()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)
	defer pc.Close()

	peerID := uuid.New()
	peer := &PeerConnection{
		ID:        peerID,
		PC:        pc,
		CameraID:  "cam-1",
		CloseChan: make(chan bool),
	}

	m.peersMu.Lock()
	m.peers[peerID] = peer
	m.peersMu.Unlock()

	// The current implementation returns the first peer in the map
	found := m.findPeerBySessionID("any-session")
	assert.NotNil(t, found)
	assert.Equal(t, peerID, found.ID)
}

// --- Tests: closePeerBySessionID ---

func TestClosePeerBySessionID_WithPeer(t *testing.T) {
	m := newTestManager()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)

	peerID := uuid.New()
	m.peersMu.Lock()
	m.peers[peerID] = &PeerConnection{
		ID:        peerID,
		PC:        pc,
		CloseChan: make(chan bool),
	}
	m.peersMu.Unlock()

	m.closePeerBySessionID("any")

	m.peersMu.RLock()
	count := len(m.peers)
	m.peersMu.RUnlock()
	assert.Equal(t, 0, count)
}

func TestClosePeerBySessionID_NoPeers(t *testing.T) {
	m := newTestManager()

	// Should not panic
	m.closePeerBySessionID("nonexistent")
}

// --- Tests: SignalingMessage / type structures ---

func TestSignalingMessage_JSONRoundTrip(t *testing.T) {
	original := SignalingMessage{
		Type:      "offer",
		SessionID: "session-123",
		CameraID:  "cam-456",
		Data:      json.RawMessage(`{"sdp":"v=0..."}`),
	}

	data, err := json.Marshal(original)
	require.NoError(t, err)

	var decoded SignalingMessage
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, original.Type, decoded.Type)
	assert.Equal(t, original.SessionID, decoded.SessionID)
	assert.Equal(t, original.CameraID, decoded.CameraID)
}

func TestOfferRequest_Fields(t *testing.T) {
	req := OfferRequest{
		SessionID: "sess-1",
		CameraID:  "cam-1",
		Offer: webrtc.SessionDescription{
			Type: webrtc.SDPTypeOffer,
			SDP:  "v=0...",
		},
	}

	assert.Equal(t, "sess-1", req.SessionID)
	assert.Equal(t, webrtc.SDPTypeOffer, req.Offer.Type)
}

func TestICECandidateRequest_Fields(t *testing.T) {
	candidateStr := "candidate:1 1 UDP 2130706431 192.168.1.1 5000 typ host"
	req := ICECandidateRequest{
		SessionID: "sess-1",
		Candidate: webrtc.ICECandidateInit{
			Candidate: candidateStr,
		},
	}

	assert.Equal(t, "sess-1", req.SessionID)
	assert.Equal(t, candidateStr, req.Candidate.Candidate)
}

// --- Tests: PeerStats / StreamInfo ---

func TestStreamInfo_Fields(t *testing.T) {
	info := StreamInfo{
		SessionID: "s1",
		CameraID:  "c1",
		State:     "connected",
		StartTime: time.Now(),
		Duration:  "5m30s",
		Stats: PeerStats{
			BytesSent:     1024000,
			BytesReceived: 512,
			PacketsLost:   3,
			Jitter:        0.015,
			RTT:           0.045,
		},
	}

	assert.Equal(t, uint64(1024000), info.Stats.BytesSent)
	assert.Equal(t, uint64(3), info.Stats.PacketsLost)
	assert.InDelta(t, 0.015, info.Stats.Jitter, 0.001)
}

// --- Tests: cleanupStaleConnections ---

func TestCleanupStaleConnections_ClosedPeer(t *testing.T) {
	m := newTestManager()

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{})
	require.NoError(t, err)

	peerID := uuid.New()
	m.peers[peerID] = &PeerConnection{
		ID:        peerID,
		PC:        pc,
		CloseChan: make(chan bool),
	}

	// Close the peer connection so its state becomes Closed
	pc.Close()

	// Allow state change to propagate
	time.Sleep(50 * time.Millisecond)

	m.cleanupStaleConnections()

	assert.Empty(t, m.peers)
}
