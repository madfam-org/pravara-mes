package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Vector3 represents a 3D vector or position
type Vector3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Rotation represents 3D rotation in degrees
type Rotation struct {
	X float64 `json:"x"` // Pitch
	Y float64 `json:"y"` // Yaw
	Z float64 `json:"z"` // Roll
}

// BoundingBox represents the 3D bounding box of a model
type BoundingBox struct {
	Min Vector3 `json:"min"`
	Max Vector3 `json:"max"`
}

// MachineModel represents a 3D model for a machine type
type MachineModel struct {
	ID           uuid.UUID   `json:"id"`
	MachineType  string      `json:"machine_type"`
	Name         string      `json:"name"`
	ModelURL     string      `json:"model_url"`     // URL to GLTF/STL file
	ThumbnailURL string      `json:"thumbnail_url"` // Preview image
	BoundingBox  BoundingBox `json:"bounding_box"`
	OriginOffset Vector3     `json:"origin_offset"` // Calibration offset
	Scale        float64     `json:"scale"`
	LODLevels    []LODLevel  `json:"lod_levels"`
	Materials    []Material  `json:"materials"`
	Animations   []Animation `json:"animations"`
	CreatedAt    time.Time   `json:"created_at"`
	UpdatedAt    time.Time   `json:"updated_at"`
}

// LODLevel represents a level of detail for the model
type LODLevel struct {
	Distance    float64 `json:"distance"`     // Distance at which to use this LOD
	ModelURL    string  `json:"model_url"`    // URL to simplified model
	VertexCount int     `json:"vertex_count"` // Number of vertices
}

// Material represents a material definition for the model
type Material struct {
	Name      string  `json:"name"`
	Color     string  `json:"color"`     // Hex color
	Metalness float64 `json:"metalness"` // 0-1
	Roughness float64 `json:"roughness"` // 0-1
	Opacity   float64 `json:"opacity"`   // 0-1
	Emissive  string  `json:"emissive"`  // Hex color for emission
}

// Animation represents an animation sequence for the model
type Animation struct {
	Name     string  `json:"name"`
	Duration float64 `json:"duration"` // Seconds
	Loop     bool    `json:"loop"`
	Type     string  `json:"type"` // "operation", "idle", "error"
}

// MachinePosition represents a machine's position in the factory
type MachinePosition struct {
	MachineID uuid.UUID `json:"machine_id"`
	Position  Vector3   `json:"position"`
	Rotation  Rotation  `json:"rotation"`
	Scale     float64   `json:"scale"`
	Visible   bool      `json:"visible"`
}

// CameraPreset represents a saved camera position
type CameraPreset struct {
	Name     string  `json:"name"`
	Position Vector3 `json:"position"`
	Target   Vector3 `json:"target"`
	FOV      float64 `json:"fov"`
	Type     string  `json:"type"` // "perspective", "orthographic"
}

// LightConfig represents lighting configuration
type LightConfig struct {
	Ambient   AmbientLight    `json:"ambient"`
	Lights    []Light         `json:"lights"`
	Shadows   bool            `json:"shadows"`
	ShadowMap ShadowMapConfig `json:"shadow_map"`
}

// AmbientLight represents ambient lighting
type AmbientLight struct {
	Color     string  `json:"color"`
	Intensity float64 `json:"intensity"`
}

// Light represents a light source
type Light struct {
	Type       string  `json:"type"` // "directional", "point", "spot"
	Position   Vector3 `json:"position"`
	Color      string  `json:"color"`
	Intensity  float64 `json:"intensity"`
	CastShadow bool    `json:"cast_shadow"`
}

// ShadowMapConfig represents shadow map settings
type ShadowMapConfig struct {
	Resolution int    `json:"resolution"` // 512, 1024, 2048, 4096
	Type       string `json:"type"`       // "PCF", "PCSS", "VSM"
}

// FactoryLayout represents a factory floor layout
type FactoryLayout struct {
	ID               uuid.UUID         `json:"id"`
	TenantID         uuid.UUID         `json:"tenant_id"`
	Name             string            `json:"name"`
	Description      string            `json:"description"`
	FloorPlan        FloorPlan         `json:"floor_plan"`
	MachinePositions []MachinePosition `json:"machine_positions"`
	CameraPresets    []CameraPreset    `json:"camera_presets"`
	LightingConfig   LightConfig       `json:"lighting_config"`
	GridSettings     GridSettings      `json:"grid_settings"`
	Zones            []Zone            `json:"zones"`
	Waypoints        []Waypoint        `json:"waypoints"`
	CreatedAt        time.Time         `json:"created_at"`
	UpdatedAt        time.Time         `json:"updated_at"`
}

// FloorPlan represents the 2D floor plan
type FloorPlan struct {
	Width     float64    `json:"width"`  // Meters
	Height    float64    `json:"height"` // Meters
	ImageURL  string     `json:"image_url"`
	Walls     []Wall     `json:"walls"`
	Obstacles []Obstacle `json:"obstacles"`
}

// Wall represents a wall in the floor plan
type Wall struct {
	Start     Vector3 `json:"start"`
	End       Vector3 `json:"end"`
	Height    float64 `json:"height"`
	Thickness float64 `json:"thickness"`
}

// Obstacle represents a static obstacle
type Obstacle struct {
	Position    Vector3     `json:"position"`
	BoundingBox BoundingBox `json:"bounding_box"`
	Type        string      `json:"type"` // "pillar", "equipment", "storage"
}

// GridSettings represents the factory grid configuration
type GridSettings struct {
	Visible    bool    `json:"visible"`
	Size       float64 `json:"size"`      // Grid cell size in meters
	Divisions  int     `json:"divisions"` // Subdivisions per cell
	Color      string  `json:"color"`     // Hex color
	Opacity    float64 `json:"opacity"`   // 0-1
	SnapToGrid bool    `json:"snap_to_grid"`
}

// Zone represents a logical zone in the factory
type Zone struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	Type         string    `json:"type"`     // "production", "storage", "quality", "shipping"
	Boundary     []Vector3 `json:"boundary"` // Polygon vertices
	Color        string    `json:"color"`
	Opacity      float64   `json:"opacity"`
	Restrictions []string  `json:"restrictions"` // Access restrictions
}

// Waypoint represents a navigation waypoint
type Waypoint struct {
	ID       uuid.UUID `json:"id"`
	Name     string    `json:"name"`
	Position Vector3   `json:"position"`
	Type     string    `json:"type"` // "loading", "unloading", "parking", "charging"
}

// Manager handles model and layout operations
type Manager struct {
	db  *sql.DB
	log *logrus.Logger
}

// NewManager creates a new model manager
func NewManager(db *sql.DB, log *logrus.Logger) *Manager {
	return &Manager{
		db:  db,
		log: log,
	}
}

// ListModels retrieves all machine models
func (m *Manager) ListModels(ctx context.Context) ([]MachineModel, error) {
	query := `
		SELECT id, machine_type, name, model_url, thumbnail_url,
		       bounding_box, origin_offset, scale, lod_levels,
		       materials, animations, created_at, updated_at
		FROM machine_models
		ORDER BY machine_type, name
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query models: %w", err)
	}
	defer rows.Close()

	var models []MachineModel
	for rows.Next() {
		var model MachineModel
		var boundingBox, originOffset, lodLevels, materials, animations []byte

		err := rows.Scan(
			&model.ID, &model.MachineType, &model.Name,
			&model.ModelURL, &model.ThumbnailURL,
			&boundingBox, &originOffset, &model.Scale,
			&lodLevels, &materials, &animations,
			&model.CreatedAt, &model.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan model: %w", err)
		}

		// Parse JSON fields
		json.Unmarshal(boundingBox, &model.BoundingBox)
		json.Unmarshal(originOffset, &model.OriginOffset)
		json.Unmarshal(lodLevels, &model.LODLevels)
		json.Unmarshal(materials, &model.Materials)
		json.Unmarshal(animations, &model.Animations)

		models = append(models, model)
	}

	return models, nil
}

// GetModel retrieves a specific machine model
func (m *Manager) GetModel(ctx context.Context, id string) (*MachineModel, error) {
	modelID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid model ID: %w", err)
	}

	query := `
		SELECT id, machine_type, name, model_url, thumbnail_url,
		       bounding_box, origin_offset, scale, lod_levels,
		       materials, animations, created_at, updated_at
		FROM machine_models
		WHERE id = $1
	`

	var model MachineModel
	var boundingBox, originOffset, lodLevels, materials, animations []byte

	err = m.db.QueryRowContext(ctx, query, modelID).Scan(
		&model.ID, &model.MachineType, &model.Name,
		&model.ModelURL, &model.ThumbnailURL,
		&boundingBox, &originOffset, &model.Scale,
		&lodLevels, &materials, &animations,
		&model.CreatedAt, &model.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("model not found")
		}
		return nil, fmt.Errorf("failed to query model: %w", err)
	}

	// Parse JSON fields
	json.Unmarshal(boundingBox, &model.BoundingBox)
	json.Unmarshal(originOffset, &model.OriginOffset)
	json.Unmarshal(lodLevels, &model.LODLevels)
	json.Unmarshal(materials, &model.Materials)
	json.Unmarshal(animations, &model.Animations)

	return &model, nil
}

// CreateModel creates a new machine model
func (m *Manager) CreateModel(ctx context.Context, model *MachineModel) error {
	model.ID = uuid.New()
	model.CreatedAt = time.Now()
	model.UpdatedAt = time.Now()

	// Marshal JSON fields
	boundingBox, _ := json.Marshal(model.BoundingBox)
	originOffset, _ := json.Marshal(model.OriginOffset)
	lodLevels, _ := json.Marshal(model.LODLevels)
	materials, _ := json.Marshal(model.Materials)
	animations, _ := json.Marshal(model.Animations)

	query := `
		INSERT INTO machine_models (
			id, machine_type, name, model_url, thumbnail_url,
			bounding_box, origin_offset, scale, lod_levels,
			materials, animations, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := m.db.ExecContext(ctx, query,
		model.ID, model.MachineType, model.Name,
		model.ModelURL, model.ThumbnailURL,
		boundingBox, originOffset, model.Scale,
		lodLevels, materials, animations,
		model.CreatedAt, model.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create model: %w", err)
	}

	return nil
}

// UpdateModel updates an existing machine model
func (m *Manager) UpdateModel(ctx context.Context, id string, model *MachineModel) error {
	modelID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid model ID: %w", err)
	}

	model.UpdatedAt = time.Now()

	// Marshal JSON fields
	boundingBox, _ := json.Marshal(model.BoundingBox)
	originOffset, _ := json.Marshal(model.OriginOffset)
	lodLevels, _ := json.Marshal(model.LODLevels)
	materials, _ := json.Marshal(model.Materials)
	animations, _ := json.Marshal(model.Animations)

	query := `
		UPDATE machine_models SET
			machine_type = $2, name = $3, model_url = $4, thumbnail_url = $5,
			bounding_box = $6, origin_offset = $7, scale = $8, lod_levels = $9,
			materials = $10, animations = $11, updated_at = $12
		WHERE id = $1
	`

	result, err := m.db.ExecContext(ctx, query,
		modelID, model.MachineType, model.Name,
		model.ModelURL, model.ThumbnailURL,
		boundingBox, originOffset, model.Scale,
		lodLevels, materials, animations,
		model.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update model: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("model not found")
	}

	return nil
}

// DeleteModel deletes a machine model
func (m *Manager) DeleteModel(ctx context.Context, id string) error {
	modelID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid model ID: %w", err)
	}

	query := `DELETE FROM machine_models WHERE id = $1`
	result, err := m.db.ExecContext(ctx, query, modelID)
	if err != nil {
		return fmt.Errorf("failed to delete model: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("model not found")
	}

	return nil
}

// ListLayouts retrieves all factory layouts
func (m *Manager) ListLayouts(ctx context.Context) ([]FactoryLayout, error) {
	query := `
		SELECT id, tenant_id, name, description, floor_plan,
		       machine_positions, camera_presets, lighting_config,
		       grid_settings, zones, waypoints, created_at, updated_at
		FROM factory_layouts
		ORDER BY name
	`

	rows, err := m.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query layouts: %w", err)
	}
	defer rows.Close()

	var layouts []FactoryLayout
	for rows.Next() {
		var layout FactoryLayout
		var floorPlan, machinePositions, cameraPresets, lightingConfig,
			gridSettings, zones, waypoints []byte

		err := rows.Scan(
			&layout.ID, &layout.TenantID, &layout.Name, &layout.Description,
			&floorPlan, &machinePositions, &cameraPresets, &lightingConfig,
			&gridSettings, &zones, &waypoints,
			&layout.CreatedAt, &layout.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan layout: %w", err)
		}

		// Parse JSON fields
		json.Unmarshal(floorPlan, &layout.FloorPlan)
		json.Unmarshal(machinePositions, &layout.MachinePositions)
		json.Unmarshal(cameraPresets, &layout.CameraPresets)
		json.Unmarshal(lightingConfig, &layout.LightingConfig)
		json.Unmarshal(gridSettings, &layout.GridSettings)
		json.Unmarshal(zones, &layout.Zones)
		json.Unmarshal(waypoints, &layout.Waypoints)

		layouts = append(layouts, layout)
	}

	return layouts, nil
}

// GetLayout retrieves a specific factory layout
func (m *Manager) GetLayout(ctx context.Context, id string) (*FactoryLayout, error) {
	layoutID, err := uuid.Parse(id)
	if err != nil {
		return nil, fmt.Errorf("invalid layout ID: %w", err)
	}

	query := `
		SELECT id, tenant_id, name, description, floor_plan,
		       machine_positions, camera_presets, lighting_config,
		       grid_settings, zones, waypoints, created_at, updated_at
		FROM factory_layouts
		WHERE id = $1
	`

	var layout FactoryLayout
	var floorPlan, machinePositions, cameraPresets, lightingConfig,
		gridSettings, zones, waypoints []byte

	err = m.db.QueryRowContext(ctx, query, layoutID).Scan(
		&layout.ID, &layout.TenantID, &layout.Name, &layout.Description,
		&floorPlan, &machinePositions, &cameraPresets, &lightingConfig,
		&gridSettings, &zones, &waypoints,
		&layout.CreatedAt, &layout.UpdatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("layout not found")
		}
		return nil, fmt.Errorf("failed to query layout: %w", err)
	}

	// Parse JSON fields
	json.Unmarshal(floorPlan, &layout.FloorPlan)
	json.Unmarshal(machinePositions, &layout.MachinePositions)
	json.Unmarshal(cameraPresets, &layout.CameraPresets)
	json.Unmarshal(lightingConfig, &layout.LightingConfig)
	json.Unmarshal(gridSettings, &layout.GridSettings)
	json.Unmarshal(zones, &layout.Zones)
	json.Unmarshal(waypoints, &layout.Waypoints)

	return &layout, nil
}

// CreateLayout creates a new factory layout
func (m *Manager) CreateLayout(ctx context.Context, layout *FactoryLayout) error {
	layout.ID = uuid.New()
	layout.CreatedAt = time.Now()
	layout.UpdatedAt = time.Now()

	// Marshal JSON fields
	floorPlan, _ := json.Marshal(layout.FloorPlan)
	machinePositions, _ := json.Marshal(layout.MachinePositions)
	cameraPresets, _ := json.Marshal(layout.CameraPresets)
	lightingConfig, _ := json.Marshal(layout.LightingConfig)
	gridSettings, _ := json.Marshal(layout.GridSettings)
	zones, _ := json.Marshal(layout.Zones)
	waypoints, _ := json.Marshal(layout.Waypoints)

	query := `
		INSERT INTO factory_layouts (
			id, tenant_id, name, description, floor_plan,
			machine_positions, camera_presets, lighting_config,
			grid_settings, zones, waypoints, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, err := m.db.ExecContext(ctx, query,
		layout.ID, layout.TenantID, layout.Name, layout.Description,
		floorPlan, machinePositions, cameraPresets, lightingConfig,
		gridSettings, zones, waypoints,
		layout.CreatedAt, layout.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create layout: %w", err)
	}

	return nil
}

// UpdateLayout updates an existing factory layout
func (m *Manager) UpdateLayout(ctx context.Context, id string, layout *FactoryLayout) error {
	layoutID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("invalid layout ID: %w", err)
	}

	layout.UpdatedAt = time.Now()

	// Marshal JSON fields
	floorPlan, _ := json.Marshal(layout.FloorPlan)
	machinePositions, _ := json.Marshal(layout.MachinePositions)
	cameraPresets, _ := json.Marshal(layout.CameraPresets)
	lightingConfig, _ := json.Marshal(layout.LightingConfig)
	gridSettings, _ := json.Marshal(layout.GridSettings)
	zones, _ := json.Marshal(layout.Zones)
	waypoints, _ := json.Marshal(layout.Waypoints)

	query := `
		UPDATE factory_layouts SET
			name = $2, description = $3, floor_plan = $4,
			machine_positions = $5, camera_presets = $6, lighting_config = $7,
			grid_settings = $8, zones = $9, waypoints = $10, updated_at = $11
		WHERE id = $1
	`

	result, err := m.db.ExecContext(ctx, query,
		layoutID, layout.Name, layout.Description,
		floorPlan, machinePositions, cameraPresets, lightingConfig,
		gridSettings, zones, waypoints,
		layout.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to update layout: %w", err)
	}

	rowsAffected, _ := result.RowsAffected()
	if rowsAffected == 0 {
		return fmt.Errorf("layout not found")
	}

	return nil
}
