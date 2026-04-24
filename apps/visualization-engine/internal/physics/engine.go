package physics

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// Engine handles physics simulations
type Engine struct {
	log *logrus.Logger
}

// NewEngine creates a new physics engine
func NewEngine(log *logrus.Logger) *Engine {
	return &Engine{
		log: log,
	}
}

// Vector3 represents a 3D vector
type Vector3 struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	Z float64 `json:"z"`
}

// Add adds two vectors
func (v Vector3) Add(other Vector3) Vector3 {
	return Vector3{
		X: v.X + other.X,
		Y: v.Y + other.Y,
		Z: v.Z + other.Z,
	}
}

// Subtract subtracts another vector from this one
func (v Vector3) Subtract(other Vector3) Vector3 {
	return Vector3{
		X: v.X - other.X,
		Y: v.Y - other.Y,
		Z: v.Z - other.Z,
	}
}

// Magnitude returns the magnitude of the vector
func (v Vector3) Magnitude() float64 {
	return math.Sqrt(v.X*v.X + v.Y*v.Y + v.Z*v.Z)
}

// Normalize returns a normalized version of the vector
func (v Vector3) Normalize() Vector3 {
	mag := v.Magnitude()
	if mag == 0 {
		return v
	}
	return Vector3{
		X: v.X / mag,
		Y: v.Y / mag,
		Z: v.Z / mag,
	}
}

// Distance returns the distance between two vectors
func (v Vector3) Distance(other Vector3) float64 {
	return v.Subtract(other).Magnitude()
}

// BoundingBox represents an axis-aligned bounding box
type BoundingBox struct {
	Min Vector3 `json:"min"`
	Max Vector3 `json:"max"`
}

// Contains checks if a point is inside the bounding box
func (b BoundingBox) Contains(point Vector3) bool {
	return point.X >= b.Min.X && point.X <= b.Max.X &&
		point.Y >= b.Min.Y && point.Y <= b.Max.Y &&
		point.Z >= b.Min.Z && point.Z <= b.Max.Z
}

// Intersects checks if two bounding boxes intersect
func (b BoundingBox) Intersects(other BoundingBox) bool {
	return !(b.Max.X < other.Min.X || b.Min.X > other.Max.X ||
		b.Max.Y < other.Min.Y || b.Min.Y > other.Max.Y ||
		b.Max.Z < other.Min.Z || b.Min.Z > other.Max.Z)
}

// GCodeSimulationRequest represents a request to simulate G-code
type GCodeSimulationRequest struct {
	MachineID     uuid.UUID     `json:"machine_id"`
	GCode         string        `json:"gcode"`
	FeedRateScale float64       `json:"feed_rate_scale"` // Speed multiplier
	WorkpieceSize BoundingBox   `json:"workpiece_size"`
	ToolDiameter  float64       `json:"tool_diameter"`
	Material      MaterialProps `json:"material"`
}

// MaterialProps represents material properties
type MaterialProps struct {
	Type         string  `json:"type"`          // "aluminum", "steel", "wood", "plastic"
	Hardness     float64 `json:"hardness"`      // 0-1 scale
	Density      float64 `json:"density"`       // kg/m³
	ChipLoad     float64 `json:"chip_load"`     // mm/tooth
	CuttingSpeed float64 `json:"cutting_speed"` // m/min
}

// GCodeSimulationResult represents the result of G-code simulation
type GCodeSimulationResult struct {
	ToolPath        []ToolPathSegment `json:"tool_path"`
	CycleTime       time.Duration     `json:"cycle_time"`
	Distance        float64           `json:"distance"`         // Total distance traveled
	CuttingTime     time.Duration     `json:"cutting_time"`     // Time spent cutting
	RapidTime       time.Duration     `json:"rapid_time"`       // Time spent in rapid moves
	BoundingBox     BoundingBox       `json:"bounding_box"`     // Bounding box of tool path
	MaterialRemoved float64           `json:"material_removed"` // Volume of material removed
	Collisions      []Collision       `json:"collisions"`
	Warnings        []string          `json:"warnings"`
}

// ToolPathSegment represents a segment of the tool path
type ToolPathSegment struct {
	Start        Vector3       `json:"start"`
	End          Vector3       `json:"end"`
	Type         string        `json:"type"` // "rapid", "linear", "arc"
	FeedRate     float64       `json:"feed_rate"`
	SpindleSpeed float64       `json:"spindle_speed"`
	Duration     time.Duration `json:"duration"`
	IsCutting    bool          `json:"is_cutting"`
}

// Collision represents a detected collision
type Collision struct {
	Position Vector3 `json:"position"`
	Type     string  `json:"type"`     // "tool", "holder", "spindle"
	Object   string  `json:"object"`   // What was hit
	Severity string  `json:"severity"` // "warning", "error", "critical"
}

// SimulateGCode simulates G-code execution
func (e *Engine) SimulateGCode(ctx context.Context, req GCodeSimulationRequest) (*GCodeSimulationResult, error) {
	e.log.Infof("Simulating G-code for machine %s", req.MachineID)

	result := &GCodeSimulationResult{
		ToolPath: make([]ToolPathSegment, 0),
		Warnings: make([]string, 0),
	}

	// Parse G-code
	lines := strings.Split(req.GCode, "\n")
	currentPos := Vector3{X: 0, Y: 0, Z: 0}
	feedRate := 100.0 // Default feed rate mm/min
	spindleSpeed := 0.0
	_ = true // rapidMode tracking (reserved for future use)
	absoluteMode := true

	minPos := Vector3{X: math.MaxFloat64, Y: math.MaxFloat64, Z: math.MaxFloat64}
	maxPos := Vector3{X: -math.MaxFloat64, Y: -math.MaxFloat64, Z: -math.MaxFloat64}

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "(") {
			continue // Skip empty lines and comments
		}

		// Parse G-code command
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		cmd := fields[0]
		params := make(map[string]float64)

		// Extract parameters
		for _, field := range fields[1:] {
			if len(field) > 1 {
				key := string(field[0])
				value := field[1:]
				if val, err := parseFloat(value); err == nil {
					params[key] = val
				}
			}
		}

		// Process command
		switch cmd {
		case "G0": // Rapid positioning
			newPos := updatePosition(currentPos, params, absoluteMode)
			segment := ToolPathSegment{
				Start:        currentPos,
				End:          newPos,
				Type:         "rapid",
				FeedRate:     10000, // Rapid speed
				SpindleSpeed: spindleSpeed,
				IsCutting:    false,
			}
			segment.Duration = calculateDuration(currentPos, newPos, 10000)
			result.ToolPath = append(result.ToolPath, segment)
			result.RapidTime += segment.Duration
			result.Distance += currentPos.Distance(newPos)
			currentPos = newPos
			updateBounds(&minPos, &maxPos, newPos)

		case "G1": // Linear interpolation
			if f, ok := params["F"]; ok {
				feedRate = f * req.FeedRateScale
			}
			newPos := updatePosition(currentPos, params, absoluteMode)
			segment := ToolPathSegment{
				Start:        currentPos,
				End:          newPos,
				Type:         "linear",
				FeedRate:     feedRate,
				SpindleSpeed: spindleSpeed,
				IsCutting:    spindleSpeed > 0 && newPos.Z < 0, // Below Z0 with spindle on
			}
			segment.Duration = calculateDuration(currentPos, newPos, feedRate)
			result.ToolPath = append(result.ToolPath, segment)

			if segment.IsCutting {
				result.CuttingTime += segment.Duration
				// Calculate material removal (simplified)
				pathLength := currentPos.Distance(newPos)
				result.MaterialRemoved += pathLength * req.ToolDiameter * math.Abs(newPos.Z-currentPos.Z) / 1000000 // mm³ to m³
			}

			result.Distance += currentPos.Distance(newPos)
			currentPos = newPos
			updateBounds(&minPos, &maxPos, newPos)

		case "G2", "G3": // Arc interpolation (clockwise/counter-clockwise)
			if f, ok := params["F"]; ok {
				feedRate = f * req.FeedRateScale
			}
			newPos := updatePosition(currentPos, params, absoluteMode)

			// Get arc center offsets (I, J relative to current position)
			iOffset := 0.0
			jOffset := 0.0
			if val, ok := params["I"]; ok {
				iOffset = val
			}
			if val, ok := params["J"]; ok {
				jOffset = val
			}

			// Calculate center of arc
			center := Vector3{
				X: currentPos.X + iOffset,
				Y: currentPos.Y + jOffset,
				Z: currentPos.Z,
			}

			// Calculate start and end angles
			startAngle := math.Atan2(currentPos.Y-center.Y, currentPos.X-center.X)
			endAngle := math.Atan2(newPos.Y-center.Y, newPos.X-center.X)
			radius := math.Sqrt(iOffset*iOffset + jOffset*jOffset)

			if radius < 0.001 {
				// Degenerate arc, treat as linear move
				segment := ToolPathSegment{
					Start:        currentPos,
					End:          newPos,
					Type:         "arc",
					FeedRate:     feedRate,
					SpindleSpeed: spindleSpeed,
					IsCutting:    spindleSpeed > 0 && newPos.Z < 0,
				}
				segment.Duration = calculateDuration(currentPos, newPos, feedRate)
				result.ToolPath = append(result.ToolPath, segment)
				result.Distance += currentPos.Distance(newPos)
				if segment.IsCutting {
					result.CuttingTime += segment.Duration
				}
				updateBounds(&minPos, &maxPos, newPos)
				currentPos = newPos
				break
			}

			// Determine sweep direction and angle
			var sweepAngle float64
			clockwise := cmd == "G2"
			if clockwise {
				sweepAngle = startAngle - endAngle
				if sweepAngle <= 0 {
					sweepAngle += 2 * math.Pi
				}
			} else {
				sweepAngle = endAngle - startAngle
				if sweepAngle <= 0 {
					sweepAngle += 2 * math.Pi
				}
			}

			// Subdivide arc into segments (~1-degree resolution)
			resolution := math.Pi / 180.0 // 1 degree in radians
			numSegments := int(math.Ceil(sweepAngle / resolution))
			if numSegments < 1 {
				numSegments = 1
			}

			// Z interpolation per segment
			zStep := (newPos.Z - currentPos.Z) / float64(numSegments)
			angleStep := sweepAngle / float64(numSegments)

			prevPoint := currentPos
			for i := 1; i <= numSegments; i++ {
				var angle float64
				if clockwise {
					angle = startAngle - angleStep*float64(i)
				} else {
					angle = startAngle + angleStep*float64(i)
				}

				var point Vector3
				if i == numSegments {
					point = newPos // Ensure we end exactly at target
				} else {
					point = Vector3{
						X: center.X + radius*math.Cos(angle),
						Y: center.Y + radius*math.Sin(angle),
						Z: currentPos.Z + zStep*float64(i),
					}
				}

				segment := ToolPathSegment{
					Start:        prevPoint,
					End:          point,
					Type:         "arc",
					FeedRate:     feedRate,
					SpindleSpeed: spindleSpeed,
					IsCutting:    spindleSpeed > 0 && point.Z < 0,
				}
				segment.Duration = calculateDuration(prevPoint, point, feedRate)
				result.ToolPath = append(result.ToolPath, segment)

				segDist := prevPoint.Distance(point)
				result.Distance += segDist

				if segment.IsCutting {
					result.CuttingTime += segment.Duration
					result.MaterialRemoved += segDist * req.ToolDiameter * math.Abs(point.Z-prevPoint.Z) / 1000000
				}

				updateBounds(&minPos, &maxPos, point)
				prevPoint = point
			}

			currentPos = newPos

		case "G90": // Absolute positioning
			absoluteMode = true

		case "G91": // Relative positioning
			absoluteMode = false

		case "M3": // Spindle on clockwise
			if s, ok := params["S"]; ok {
				spindleSpeed = s
			}

		case "M5": // Spindle off
			spindleSpeed = 0

		case "F": // Set feed rate
			if len(fields) > 1 {
				if val, err := parseFloat(fields[1]); err == nil {
					feedRate = val * req.FeedRateScale
				}
			}
		}
	}

	// Set bounding box
	result.BoundingBox = BoundingBox{Min: minPos, Max: maxPos}

	// Calculate total cycle time
	result.CycleTime = result.CuttingTime + result.RapidTime

	// Check for collisions with workpiece bounds
	if req.WorkpieceSize.Min.X != 0 || req.WorkpieceSize.Max.X != 0 {
		for _, segment := range result.ToolPath {
			if !req.WorkpieceSize.Contains(segment.End) {
				result.Collisions = append(result.Collisions, Collision{
					Position: segment.End,
					Type:     "tool",
					Object:   "workpiece_boundary",
					Severity: "warning",
				})
			}
		}
	}

	e.log.Infof("G-code simulation complete: %d segments, %.2f minutes cycle time",
		len(result.ToolPath), result.CycleTime.Minutes())

	return result, nil
}

// CollisionCheckRequest represents a collision check request
type CollisionCheckRequest struct {
	MachineID uuid.UUID     `json:"machine_id"`
	ToolPath  []Vector3     `json:"tool_path"`
	Obstacles []BoundingBox `json:"obstacles"`
	ToolSize  BoundingBox   `json:"tool_size"`
}

// CollisionCheckResult represents collision check results
type CollisionCheckResult struct {
	Collisions []Collision `json:"collisions"`
	Safe       bool        `json:"safe"`
}

// CheckCollisions checks for collisions along a tool path
func (e *Engine) CheckCollisions(ctx context.Context, req CollisionCheckRequest) (*CollisionCheckResult, error) {
	result := &CollisionCheckResult{
		Collisions: make([]Collision, 0),
		Safe:       true,
	}

	// Check each segment of the tool path
	for i := 0; i < len(req.ToolPath)-1; i++ {
		_ = req.ToolPath[i] // start point reserved for swept-volume collision
		end := req.ToolPath[i+1]

		// Create bounding box for tool at each position
		toolBox := BoundingBox{
			Min: Vector3{
				X: end.X + req.ToolSize.Min.X,
				Y: end.Y + req.ToolSize.Min.Y,
				Z: end.Z + req.ToolSize.Min.Z,
			},
			Max: Vector3{
				X: end.X + req.ToolSize.Max.X,
				Y: end.Y + req.ToolSize.Max.Y,
				Z: end.Z + req.ToolSize.Max.Z,
			},
		}

		// Check against obstacles
		for j, obstacle := range req.Obstacles {
			if toolBox.Intersects(obstacle) {
				result.Collisions = append(result.Collisions, Collision{
					Position: end,
					Type:     "tool",
					Object:   fmt.Sprintf("obstacle_%d", j),
					Severity: "error",
				})
				result.Safe = false
			}
		}
	}

	return result, nil
}

// MaterialSimulationRequest represents a material simulation request
type MaterialSimulationRequest struct {
	Process      string        `json:"process"` // "milling", "turning", "3d_printing", "laser_cutting"
	Material     MaterialProps `json:"material"`
	ToolPath     []Vector3     `json:"tool_path"`
	ToolDiameter float64       `json:"tool_diameter"`
	FeedRate     float64       `json:"feed_rate"`
	SpindleSpeed float64       `json:"spindle_speed"`
	LayerHeight  float64       `json:"layer_height"` // For 3D printing
}

// MaterialSimulationResult represents material simulation results
type MaterialSimulationResult struct {
	MaterialState  MaterialState  `json:"material_state"`
	SurfaceQuality float64        `json:"surface_quality"` // 0-1 scale
	ToolWear       float64        `json:"tool_wear"`       // 0-1 scale
	Temperature    TemperatureMap `json:"temperature"`
	Forces         CuttingForces  `json:"forces"`
	ChipFormation  ChipParameters `json:"chip_formation"`
}

// MaterialState represents the state of material after processing
type MaterialState struct {
	RemovedVolume   float64     `json:"removed_volume"`   // m³
	RemainingVolume float64     `json:"remaining_volume"` // m³
	SurfaceArea     float64     `json:"surface_area"`     // m²
	Dimensions      BoundingBox `json:"dimensions"`
}

// TemperatureMap represents temperature distribution
type TemperatureMap struct {
	MaxTemp   float64    `json:"max_temp"` // °C
	AvgTemp   float64    `json:"avg_temp"` // °C
	HeatZones []HeatZone `json:"heat_zones"`
}

// HeatZone represents a localized heat zone
type HeatZone struct {
	Position    Vector3 `json:"position"`
	Temperature float64 `json:"temperature"` // °C
	Radius      float64 `json:"radius"`      // mm
}

// CuttingForces represents cutting force components
type CuttingForces struct {
	Tangential float64 `json:"tangential"` // N
	Radial     float64 `json:"radial"`     // N
	Axial      float64 `json:"axial"`      // N
	Resultant  float64 `json:"resultant"`  // N
	Power      float64 `json:"power"`      // W
}

// ChipParameters represents chip formation parameters
type ChipParameters struct {
	ChipThickness  float64 `json:"chip_thickness"`   // mm
	ChipCurlRadius float64 `json:"chip_curl_radius"` // mm
	ChipBreaking   bool    `json:"chip_breaking"`
	ChipEvacuation string  `json:"chip_evacuation"` // "good", "moderate", "poor"
}

// SimulateMaterial simulates material processing
func (e *Engine) SimulateMaterial(ctx context.Context, req MaterialSimulationRequest) (*MaterialSimulationResult, error) {
	e.log.Infof("Simulating %s process for %s material", req.Process, req.Material.Type)

	result := &MaterialSimulationResult{}

	switch req.Process {
	case "milling", "turning":
		result = e.simulateSubtractive(req)
	case "3d_printing":
		result = e.simulateAdditive(req)
	case "laser_cutting":
		result = e.simulateThermalCutting(req)
	default:
		return nil, fmt.Errorf("unsupported process: %s", req.Process)
	}

	return result, nil
}

// simulateSubtractive simulates subtractive manufacturing
func (e *Engine) simulateSubtractive(req MaterialSimulationRequest) *MaterialSimulationResult {
	result := &MaterialSimulationResult{}

	// Calculate chip thickness
	feedPerTooth := req.FeedRate / (req.SpindleSpeed * 2) // Assuming 2-flute tool
	chipThickness := feedPerTooth * math.Sin(math.Pi/4)   // 45° average engagement

	// Calculate cutting forces (simplified Merchant's equation)
	specificCuttingForce := req.Material.Hardness * 2000 // N/mm² (simplified)
	chipArea := chipThickness * req.ToolDiameter
	tangentialForce := specificCuttingForce * chipArea

	result.Forces = CuttingForces{
		Tangential: tangentialForce,
		Radial:     tangentialForce * 0.3, // Typical ratio
		Axial:      tangentialForce * 0.2,
		Resultant:  tangentialForce * 1.1,
		Power:      tangentialForce * (req.SpindleSpeed * req.ToolDiameter * math.Pi / 60000), // W
	}

	// Calculate temperature (simplified)
	cuttingSpeed := req.SpindleSpeed * req.ToolDiameter * math.Pi / 1000 // m/min
	maxTemp := 200 + cuttingSpeed*10 + req.Material.Hardness*500

	result.Temperature = TemperatureMap{
		MaxTemp: maxTemp,
		AvgTemp: maxTemp * 0.6,
		HeatZones: []HeatZone{
			{
				Position:    req.ToolPath[len(req.ToolPath)-1], // Current position
				Temperature: maxTemp,
				Radius:      req.ToolDiameter * 2,
			},
		},
	}

	// Chip formation
	result.ChipFormation = ChipParameters{
		ChipThickness:  chipThickness,
		ChipCurlRadius: chipThickness * 10,
		ChipBreaking:   chipThickness < 0.1, // Breaks if thin enough
		ChipEvacuation: "good",              // Simplified
	}

	// Surface quality (Ra roughness estimation)
	surfaceRoughness := math.Pow(req.FeedRate, 2) / (32 * req.ToolDiameter)
	result.SurfaceQuality = 1.0 - math.Min(surfaceRoughness/10, 1.0) // Convert to 0-1 scale

	// Tool wear (simplified)
	result.ToolWear = math.Min(cuttingSpeed*req.Material.Hardness/1000, 1.0)

	// Material state
	pathLength := calculatePathLength(req.ToolPath)
	removedVolume := pathLength * req.ToolDiameter * req.ToolDiameter / 4 * math.Pi / 1e9 // mm³ to m³

	result.MaterialState = MaterialState{
		RemovedVolume: removedVolume,
		// Other fields would require workpiece geometry
	}

	return result
}

// simulateAdditive simulates additive manufacturing (3D printing)
func (e *Engine) simulateAdditive(req MaterialSimulationRequest) *MaterialSimulationResult {
	result := &MaterialSimulationResult{}

	// Calculate extrusion parameters
	extrusionWidth := req.ToolDiameter * 1.2 // Typical ratio
	layerArea := extrusionWidth * req.LayerHeight

	// Calculate deposition rate
	pathLength := calculatePathLength(req.ToolPath)
	depositionVolume := pathLength * layerArea / 1e9 // mm³ to m³
	_ = pathLength / req.FeedRate * 60               // depositionTime (seconds), reserved for thermal model

	// Temperature for thermoplastics
	extrusionTemp := 200.0 // Default for PLA
	if req.Material.Type == "abs" {
		extrusionTemp = 240.0
	} else if req.Material.Type == "petg" {
		extrusionTemp = 250.0
	}

	result.Temperature = TemperatureMap{
		MaxTemp: extrusionTemp,
		AvgTemp: extrusionTemp * 0.8,
		HeatZones: []HeatZone{
			{
				Position:    req.ToolPath[len(req.ToolPath)-1],
				Temperature: extrusionTemp,
				Radius:      req.ToolDiameter,
			},
		},
	}

	// Layer adhesion quality
	coolingRate := 10.0                              // °C/s
	layerBondStrength := 1.0 - (coolingRate / 100.0) // Simplified
	result.SurfaceQuality = layerBondStrength

	// Material state
	result.MaterialState = MaterialState{
		RemovedVolume:   0, // Nothing removed in additive
		RemainingVolume: depositionVolume,
	}

	return result
}

// simulateThermalCutting simulates laser/plasma cutting
func (e *Engine) simulateThermalCutting(req MaterialSimulationRequest) *MaterialSimulationResult {
	result := &MaterialSimulationResult{}

	// Laser power and cutting speed relationship
	cuttingSpeed := req.FeedRate / 60               // mm/s
	_ = req.Material.Density * cuttingSpeed * 0.001 // requiredPower (simplified), reserved for power analysis

	// Heat affected zone
	hazWidth := req.ToolDiameter + 0.5 // mm
	maxTemp := 1000.0                  // Melting temperature

	result.Temperature = TemperatureMap{
		MaxTemp: maxTemp,
		AvgTemp: maxTemp * 0.3,
		HeatZones: []HeatZone{
			{
				Position:    req.ToolPath[len(req.ToolPath)-1],
				Temperature: maxTemp,
				Radius:      hazWidth,
			},
		},
	}

	// Kerf width and quality
	kerfWidth := req.ToolDiameter
	result.SurfaceQuality = 1.0 - (kerfWidth-req.ToolDiameter)/req.ToolDiameter

	// Material removal (kerf)
	pathLength := calculatePathLength(req.ToolPath)
	removedVolume := pathLength * kerfWidth * 5 / 1e9 // Assuming 5mm thickness

	result.MaterialState = MaterialState{
		RemovedVolume: removedVolume,
	}

	return result
}

// Helper functions

func updatePosition(current Vector3, params map[string]float64, absolute bool) Vector3 {
	newPos := current

	if absolute {
		if x, ok := params["X"]; ok {
			newPos.X = x
		}
		if y, ok := params["Y"]; ok {
			newPos.Y = y
		}
		if z, ok := params["Z"]; ok {
			newPos.Z = z
		}
	} else {
		if x, ok := params["X"]; ok {
			newPos.X += x
		}
		if y, ok := params["Y"]; ok {
			newPos.Y += y
		}
		if z, ok := params["Z"]; ok {
			newPos.Z += z
		}
	}

	return newPos
}

func updateBounds(min, max *Vector3, pos Vector3) {
	min.X = math.Min(min.X, pos.X)
	min.Y = math.Min(min.Y, pos.Y)
	min.Z = math.Min(min.Z, pos.Z)

	max.X = math.Max(max.X, pos.X)
	max.Y = math.Max(max.Y, pos.Y)
	max.Z = math.Max(max.Z, pos.Z)
}

func calculateDuration(start, end Vector3, feedRate float64) time.Duration {
	distance := start.Distance(end)
	minutes := distance / feedRate
	return time.Duration(minutes * float64(time.Minute))
}

func calculatePathLength(path []Vector3) float64 {
	length := 0.0
	for i := 0; i < len(path)-1; i++ {
		length += path[i].Distance(path[i+1])
	}
	return length
}

func parseFloat(s string) (float64, error) {
	var result float64
	_, err := fmt.Sscanf(s, "%f", &result)
	return result, err
}

// parseParameters extracts key-value pairs from G-code parameter fields (e.g. ["X10.5", "Y20.3"])
func parseParameters(fields []string) map[string]float64 {
	params := make(map[string]float64)
	for _, field := range fields {
		if len(field) > 1 {
			key := string(field[0])
			value := field[1:]
			if val, err := parseFloat(value); err == nil {
				params[key] = val
			}
		}
	}
	return params
}
