package camera

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Camera represents a video camera
type Camera struct {
	ID          uuid.UUID              `json:"id"`
	TenantID    uuid.UUID              `json:"tenant_id"`
	Name        string                 `json:"name"`
	StreamURL   string                 `json:"stream_url"`
	Protocol    string                 `json:"protocol"` // rtsp, http, https, onvif, usb
	Position    Position               `json:"position"`
	Orientation Orientation            `json:"orientation"`
	FOV         float64                `json:"fov"`
	MachineID   *uuid.UUID             `json:"machine_id,omitempty"`
	Features    Features               `json:"features"`
	IsActive    bool                   `json:"is_active"`
	Status      string                 `json:"status"`
	Metadata    map[string]interface{} `json:"metadata"`
	CreatedAt   time.Time              `json:"created_at"`
	UpdatedAt   time.Time              `json:"updated_at"`
}

// Position represents 3D position
type Position struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Orientation represents camera orientation
type Orientation struct {
	Pitch float64 `json:"pitch"` // Up/down
	Yaw   float64 `json:"yaw"`   // Left/right
	Roll  float64 `json:"roll"`  // Tilt
}

// Features represents camera capabilities
type Features struct {
	PTZ         bool   `json:"ptz"` // Pan-Tilt-Zoom
	NightVision bool   `json:"night_vision"`
	Motion      bool   `json:"motion"`
	Audio       bool   `json:"audio"`
	Resolution  string `json:"resolution"` // e.g., "1920x1080"
	FPS         int    `json:"fps"`
}

// ONVIFDevice represents an ONVIF-compliant camera
type ONVIFDevice struct {
	Address      string    `json:"address"`
	Name         string    `json:"name"`
	Manufacturer string    `json:"manufacturer"`
	Model        string    `json:"model"`
	StreamURLs   []string  `json:"stream_urls"`
	Discovered   time.Time `json:"discovered"`
}

// Manager handles camera operations
type Manager struct {
	db          *sql.DB
	log         *logrus.Logger
	cameras     map[uuid.UUID]*Camera
	camerasMu   sync.RWMutex
	discoveryMu sync.Mutex
}

// NewManager creates a new camera manager
func NewManager(db *sql.DB, log *logrus.Logger) *Manager {
	return &Manager{
		db:      db,
		log:     log,
		cameras: make(map[uuid.UUID]*Camera),
	}
}

// ListCameras retrieves all cameras
func (m *Manager) ListCameras(ctx context.Context) ([]Camera, error) {
	query := `
		SELECT id, tenant_id, name, stream_url, protocol,
		       position, orientation, fov, machine_id, features,
		       is_active, created_at, updated_at
		FROM cameras
		ORDER BY name
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query cameras: %w", err)
	}
	defer rows.Close()

	var cameras []Camera
	for rows.Next() {
		var cam Camera
		var position, orientation, features []byte
		var machineID sql.NullString

		err := rows.Scan(
			&cam.ID, &cam.TenantID, &cam.Name, &cam.StreamURL, &cam.Protocol,
			&position, &orientation, &cam.FOV, &machineID, &features,
			&cam.IsActive, &cam.CreatedAt, &cam.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan camera: %w", err)
		}

		// Parse JSON fields
		json.Unmarshal(position, &cam.Position)
		json.Unmarshal(orientation, &cam.Orientation)
		json.Unmarshal(features, &cam.Features)

		if machineID.Valid {
			id, _ := uuid.Parse(machineID.String)
			cam.MachineID = &id
		}

		// Check camera status
		cam.Status = m.checkCameraStatus(&cam)

		cameras = append(cameras, cam)
	}

	return cameras, nil
}

// GetCamera retrieves a specific camera
func (m *Manager) GetCamera(ctx context.Context, id string) (*Camera, error) {
	cameraID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid camera ID: %w", err)
	}

	// Check cache first
	m.camerasMu.RLock()
	if cam, ok := m.cameras[cameraID]; ok {
		m.camerasMu.RUnlock()
		return cam, nil
	}
	m.camerasMu.RUnlock()

	query := `
		SELECT id, tenant_id, name, stream_url, protocol,
		       position, orientation, fov, machine_id, features,
		       is_active, created_at, updated_at
		FROM cameras
		WHERE id = $1
	`

	var cam Camera
	var position, orientation, features []byte
	var machineID sql.NullString

	err = m.db.QueryRowContext(ctx, query, cameraID).Scan(
		&cam.ID, &cam.TenantID, &cam.Name, &cam.StreamURL, &cam.Protocol,
		&position, &orientation, &cam.FOV, &machineID, &features,
		&cam.IsActive, &cam.CreatedAt, &cam.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("camera not found")
		}
		return nil, fmt.Errorf("failed to query camera: %w", err)
	}

	// Parse JSON fields
	json.Unmarshal(position, &cam.Position)
	json.Unmarshal(orientation, &cam.Orientation)
	json.Unmarshal(features, &cam.Features)

	if machineID.Valid {
		id, _ := uuid.Parse(machineID.String)
		cam.MachineID = &id
	}

	// Check camera status
	cam.Status = m.checkCameraStatus(&cam)

	// Cache the camera
	m.camerasMu.Lock()
	m.cameras[cameraID] = &cam
	m.camerasMu.Unlock()

	return &cam, nil
}

// CreateCamera creates a new camera
func (m *Manager) CreateCamera(ctx context.Context, cam *Camera) error {
	cam.ID = uuid.New()
	cam.CreatedAt = time.Now()
	cam.UpdatedAt = time.Now()

	// Marshal JSON fields
	position, _ := json.Marshal(cam.Position)
	orientation, _ := json.Marshal(cam.Orientation)
	features, _ := json.Marshal(cam.Features)

	query := `
		INSERT INTO cameras (
			id, tenant_id, name, stream_url, protocol,
			position, orientation, fov, machine_id, features,
			is_active, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	var machineID *string
	if cam.MachineID != nil {
		str := cam.MachineID.String()
		machineID = &str
	}

	_, err := m.db.ExecContext(ctx, query,
		cam.ID, cam.TenantID, cam.Name, cam.StreamURL, cam.Protocol,
		position, orientation, cam.FOV, machineID, features,
		cam.IsActive, cam.CreatedAt, cam.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create camera: %w", err)
	}

	// Add to cache
	m.camerasMu.Lock()
	m.cameras[cam.ID] = cam
	m.camerasMu.Unlock()

	return nil
}

// UpdateCamera updates an existing camera
func (m *Manager) UpdateCamera(ctx context.Context, id string, cam *Camera) error {
	cameraID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid camera ID: %w", err)
	}

	cam.UpdatedAt = time.Now()

	// Marshal JSON fields
	position, _ := json.Marshal(cam.Position)
	orientation, _ := json.Marshal(cam.Orientation)
	features, _ := json.Marshal(cam.Features)

	query := `
		UPDATE cameras SET
			name = $2, stream_url = $3, protocol = $4,
			position = $5, orientation = $6, fov = $7,
			machine_id = $8, features = $9, is_active = $10,
			updated_at = $11
		WHERE id = $1
	`

	var machineID *string
	if cam.MachineID != nil {
		str := cam.MachineID.String()
		machineID = &str
	}

	result, err := m.db.ExecContext(ctx, query,
		cameraID, cam.Name, cam.StreamURL, cam.Protocol,
		position, orientation, cam.FOV, machineID, features,
		cam.IsActive, cam.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update camera: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("camera not found")
	}

	// Update cache
	m.camerasMu.Lock()
	m.cameras[cameraID] = cam
	m.camerasMu.Unlock()

	return nil
}

// DeleteCamera deletes a camera
func (m *Manager) DeleteCamera(ctx context.Context, id string) error {
	cameraID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid camera ID: %w", err)
	}

	query := `DELETE FROM cameras WHERE id = $1`
	result, err := m.db.ExecContext(ctx, query, cameraID)
	if err != nil {
		return fmt.Errorf("failed to delete camera: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("camera not found")
	}

	// Remove from cache
	m.camerasMu.Lock()
	delete(m.cameras, cameraID)
	m.camerasMu.Unlock()

	return nil
}

// DiscoverCameras discovers cameras on the network
func (m *Manager) DiscoverCameras(ctx context.Context) ([]ONVIFDevice, error) {
	m.discoveryMu.Lock()
	defer m.discoveryMu.Unlock()

	var discovered []ONVIFDevice

	// Discover ONVIF cameras
	onvifDevices := m.discoverONVIF()
	discovered = append(discovered, onvifDevices...)

	// Discover RTSP streams
	rtspStreams := m.discoverRTSP()
	for _, stream := range rtspStreams {
		discovered = append(discovered, ONVIFDevice{
			Address:    stream,
			Name:       fmt.Sprintf("RTSP Camera at %s", stream),
			StreamURLs: []string{stream},
			Discovered: time.Now(),
		})
	}

	m.log.Infof("Discovered %d cameras", len(discovered))
	return discovered, nil
}

// discoverONVIF discovers ONVIF-compliant cameras
func (m *Manager) discoverONVIF() []ONVIFDevice {
	var devices []ONVIFDevice

	// WS-Discovery probe for ONVIF devices
	// This is simplified - real implementation would use proper SOAP/WS-Discovery
	interfaces, err := net.Interfaces()
	if err != nil {
		m.log.Errorf("Failed to get network interfaces: %v", err)
		return devices
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}

			// Scan common ONVIF ports
			m.scanONVIFPorts(ipnet.IP.String(), &devices)
		}
	}

	return devices
}

// scanONVIFPorts scans common ONVIF ports
func (m *Manager) scanONVIFPorts(subnet string, devices *[]ONVIFDevice) {
	ports := []int{80, 8080, 554, 8554} // Common ONVIF/RTSP ports

	// Get subnet base
	parts := strings.Split(subnet, ".")
	if len(parts) != 4 {
		return
	}
	base := strings.Join(parts[:3], ".")

	// Scan subnet (simplified - only checking .1 to .254)
	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("%s.%d", base, i)

		for _, port := range ports {
			address := fmt.Sprintf("%s:%d", ip, port)

			// Try to connect
			conn, err := net.DialTimeout("tcp", address, 100*time.Millisecond)
			if err != nil {
				continue
			}
			conn.Close()

			// Check if it's an ONVIF device (simplified check)
			if m.isONVIFDevice(ip, port) {
				*devices = append(*devices, ONVIFDevice{
					Address:    address,
					Name:       fmt.Sprintf("Camera at %s", address),
					StreamURLs: []string{fmt.Sprintf("rtsp://%s/stream", address)},
					Discovered: time.Now(),
				})
			}
		}
	}
}

// isONVIFDevice checks if address responds to ONVIF
func (m *Manager) isONVIFDevice(ip string, port int) bool {
	// Simplified check - try to access ONVIF device service
	url := fmt.Sprintf("http://%s:%d/onvif/device_service", ip, port)

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// If we get any response, assume it might be ONVIF
	return resp.StatusCode > 0
}

// discoverRTSP discovers RTSP streams
func (m *Manager) discoverRTSP() []string {
	var streams []string

	// Common RTSP ports
	ports := []int{554, 8554}

	// Scan local subnet for RTSP streams
	interfaces, err := net.Interfaces()
	if err != nil {
		return streams
	}

	for _, iface := range interfaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP.To4() == nil {
				continue
			}

			// Scan for RTSP
			parts := strings.Split(ipnet.IP.String(), ".")
			if len(parts) != 4 {
				continue
			}
			base := strings.Join(parts[:3], ".")

			for i := 1; i <= 254; i++ {
				for _, port := range ports {
					address := fmt.Sprintf("rtsp://%s.%d:%d/", base, i, port)

					// Try to connect (simplified)
					conn, err := net.DialTimeout("tcp", fmt.Sprintf("%s.%d:%d", base, i, port), 100*time.Millisecond)
					if err != nil {
						continue
					}
					conn.Close()

					streams = append(streams, address)
				}
			}
		}
	}

	return streams
}

// checkCameraStatus checks if camera is accessible
func (m *Manager) checkCameraStatus(cam *Camera) string {
	if !cam.IsActive {
		return "disabled"
	}

	// Quick connectivity check based on protocol
	switch cam.Protocol {
	case "rtsp":
		if m.checkRTSPConnection(cam.StreamURL) {
			return "online"
		}
		return "offline"
	case "http", "https":
		if m.checkHTTPConnection(cam.StreamURL) {
			return "online"
		}
		return "offline"
	case "usb":
		// USB cameras would need different checking
		return "unknown"
	default:
		return "unknown"
	}
}

// checkRTSPConnection checks RTSP stream availability
func (m *Manager) checkRTSPConnection(url string) bool {
	// Parse RTSP URL to get host:port
	if !strings.HasPrefix(url, "rtsp://") {
		return false
	}

	urlPart := strings.TrimPrefix(url, "rtsp://")
	parts := strings.Split(urlPart, "/")
	if len(parts) == 0 {
		return false
	}

	hostPort := parts[0]
	if !strings.Contains(hostPort, ":") {
		hostPort += ":554" // Default RTSP port
	}

	// Try to connect
	conn, err := net.DialTimeout("tcp", hostPort, 2*time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// checkHTTPConnection checks HTTP stream availability
func (m *Manager) checkHTTPConnection(url string) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Head(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// StartDiscovery starts periodic camera discovery
func (m *Manager) StartDiscovery() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			ctx := context.Background()
			discovered, err := m.DiscoverCameras(ctx)
			if err != nil {
				m.log.Errorf("Camera discovery failed: %v", err)
				continue
			}

			m.log.Infof("Periodic discovery found %d cameras", len(discovered))
		}
	}
}
