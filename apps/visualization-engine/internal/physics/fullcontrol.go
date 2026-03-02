package physics

import (
	"bufio"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
)

// FullControlGCodeParser handles FullControl-specific GCODE features
type FullControlGCodeParser struct {
	log *logrus.Logger
}

// NewFullControlParser creates a new FullControl GCODE parser
func NewFullControlParser(log *logrus.Logger) *FullControlGCodeParser {
	return &FullControlGCodeParser{
		log: log,
	}
}

// ExtrusionSegment represents a 3D printing extrusion segment
type ExtrusionSegment struct {
	Start          Vector3  `json:"start"`
	End            Vector3  `json:"end"`
	ExtrusionRate  float64  `json:"extrusion_rate"`  // mm³/s
	LayerHeight    float64  `json:"layer_height"`     // mm
	LineWidth      float64  `json:"line_width"`       // mm
	Temperature    float64  `json:"temperature"`      // °C
	Speed          float64  `json:"speed"`            // mm/s
	Material       string   `json:"material"`         // PLA, ABS, PETG, etc.
	IsRetraction   bool     `json:"is_retraction"`
	IsPrime        bool     `json:"is_prime"`
	IsTravel       bool     `json:"is_travel"`
	VolumeDeposited float64 `json:"volume_deposited"` // mm³
}

// FullControlSimulationResult extends the base simulation with 3D printing specifics
type FullControlSimulationResult struct {
	GCodeSimulationResult
	Layers           []Layer           `json:"layers"`
	ExtrusionPath    []ExtrusionSegment `json:"extrusion_path"`
	TotalFilament    float64           `json:"total_filament_mm"`     // Total filament length used
	TotalVolume      float64           `json:"total_volume_mm3"`       // Total volume deposited
	PrintTime        time.Duration     `json:"print_time"`
	LayerCount       int               `json:"layer_count"`
	Retractions      int               `json:"retractions"`
	EstimatedWeight  float64           `json:"estimated_weight_grams"` // Based on material density
	NozzleTemp       float64           `json:"nozzle_temperature"`
	BedTemp          float64           `json:"bed_temperature"`
	MaterialType     string            `json:"material_type"`
}

// Layer represents a single print layer
type Layer struct {
	Number       int                `json:"number"`
	Height       float64            `json:"height"`          // Z position
	Segments     []ExtrusionSegment `json:"segments"`
	PrintTime    time.Duration      `json:"print_time"`
	FilamentUsed float64            `json:"filament_used_mm"`
	IsSupport    bool               `json:"is_support"`
	IsInfill     bool               `json:"is_infill"`
	IsPerimeter  bool               `json:"is_perimeter"`
}

// MaterialProperties defines material-specific properties
type MaterialProperties struct {
	Density           float64 `json:"density_g_cm3"`      // g/cm³
	MeltingTemp       float64 `json:"melting_temp"`        // °C
	GlassTransition   float64 `json:"glass_transition"`    // °C
	ThermalExpansion  float64 `json:"thermal_expansion"`   // coefficient
	FlowRate          float64 `json:"flow_rate_multiplier"`
	RetractionLength  float64 `json:"retraction_length"`   // mm
	RetractionSpeed   float64 `json:"retraction_speed"`    // mm/s
}

var materials = map[string]MaterialProperties{
	"PLA": {
		Density:          1.25,
		MeltingTemp:      180,
		GlassTransition:  60,
		ThermalExpansion: 0.00007,
		FlowRate:         1.0,
		RetractionLength: 0.8,
		RetractionSpeed:  40,
	},
	"ABS": {
		Density:          1.04,
		MeltingTemp:      230,
		GlassTransition:  100,
		ThermalExpansion: 0.00009,
		FlowRate:         0.95,
		RetractionLength: 1.0,
		RetractionSpeed:  40,
	},
	"PETG": {
		Density:          1.27,
		MeltingTemp:      245,
		GlassTransition:  80,
		ThermalExpansion: 0.00006,
		FlowRate:         0.98,
		RetractionLength: 1.2,
		RetractionSpeed:  35,
	},
	"TPU": {
		Density:          1.20,
		MeltingTemp:      220,
		GlassTransition:  -40,
		ThermalExpansion: 0.00015,
		FlowRate:         1.1,
		RetractionLength: 0, // No retraction for flexible
		RetractionSpeed:  0,
	},
}

// SimulateFullControlGCode simulates FullControl-generated GCODE with material deposition
func (p *FullControlGCodeParser) SimulateFullControlGCode(gcode string, material string, nozzleDiameter float64) (*FullControlSimulationResult, error) {
	result := &FullControlSimulationResult{
		MaterialType: material,
		Layers:       make([]Layer, 0),
	}

	// Get material properties
	matProps, exists := materials[material]
	if !exists {
		matProps = materials["PLA"] // Default to PLA
		result.MaterialType = "PLA"
	}

	scanner := bufio.NewScanner(strings.NewReader(gcode))

	// State tracking
	var (
		currentPos     = Vector3{X: 0, Y: 0, Z: 0}
		currentE       float64 = 0 // Extruder position
		currentLayer   *Layer
		currentLayerZ  float64 = 0
		feedRate       float64 = 60 // mm/s default
		extruding      bool = false
		absoluteMode   bool = true
		absoluteEMode  bool = true
		nozzleTemp     float64 = 200
		bedTemp        float64 = 60
		layerHeight    float64 = 0.2 // Default layer height
		lineWidth      float64 = nozzleDiameter * 1.2
		filamentDiameter float64 = 1.75 // mm
	)

	// Parse FullControl metadata comments
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines
		if line == "" {
			continue
		}

		// Parse FullControl metadata
		if strings.HasPrefix(line, ";") {
			p.parseMetadata(line, result)
			continue
		}

		// Remove inline comments
		if idx := strings.Index(line, ";"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}

		// Parse G-code command
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}

		cmd := fields[0]
		params := parseParameters(fields[1:])

		switch cmd {
		case "G0", "G1": // Move command
			newPos := updatePosition(currentPos, params, absoluteMode)

			// Check if extruding
			newE := currentE
			if e, hasE := params["E"]; hasE {
				if absoluteEMode {
					newE = e
				} else {
					newE = currentE + e
				}
			}

			// Check for layer change
			if newPos.Z != currentPos.Z {
				// New layer
				if currentLayer != nil {
					result.Layers = append(result.Layers, *currentLayer)
				}
				currentLayerZ = newPos.Z
				currentLayer = &Layer{
					Number: len(result.Layers) + 1,
					Height: newPos.Z,
				}
				layerHeight = newPos.Z - currentPos.Z
			}

			// Create segment
			segment := ExtrusionSegment{
				Start:       currentPos,
				End:         newPos,
				Speed:       feedRate,
				Temperature: nozzleTemp,
				Material:    result.MaterialType,
				LayerHeight: layerHeight,
				LineWidth:   lineWidth,
			}

			// Calculate extrusion
			if newE != currentE {
				filamentLength := math.Abs(newE - currentE)
				filamentArea := math.Pi * math.Pow(filamentDiameter/2, 2)
				volumeExtruded := filamentLength * filamentArea

				pathLength := currentPos.Distance(newPos)
				if pathLength > 0 {
					segment.ExtrusionRate = volumeExtruded / (pathLength / feedRate)
					segment.VolumeDeposited = volumeExtruded
					result.TotalFilament += filamentLength
					result.TotalVolume += volumeExtruded
				}

				// Check for retraction/prime
				if newE < currentE {
					segment.IsRetraction = true
					result.Retractions++
				} else if pathLength < 0.01 { // Very small move with extrusion
					segment.IsPrime = true
				}
			} else {
				segment.IsTravel = true
			}

			// Add to current layer
			if currentLayer != nil {
				currentLayer.Segments = append(currentLayer.Segments, segment)
				currentLayer.FilamentUsed += math.Abs(newE - currentE)
			}

			// Add to extrusion path
			result.ExtrusionPath = append(result.ExtrusionPath, segment)

			// Update position
			currentPos = newPos
			currentE = newE

		case "G90": // Absolute positioning
			absoluteMode = true

		case "G91": // Relative positioning
			absoluteMode = false

		case "M82": // Absolute extrusion mode
			absoluteEMode = true

		case "M83": // Relative extrusion mode
			absoluteEMode = false

		case "M104", "M109": // Set nozzle temperature
			if temp, hasS := params["S"]; hasS {
				nozzleTemp = temp
				result.NozzleTemp = temp
			}

		case "M140", "M190": // Set bed temperature
			if temp, hasS := params["S"]; hasS {
				bedTemp = temp
				result.BedTemp = temp
			}

		case "G92": // Set position
			if e, hasE := params["E"]; hasE {
				currentE = e
			}

		case "F": // Feed rate
			if len(fields) > 1 {
				if val, err := parseFloat(fields[1]); err == nil {
					feedRate = val / 60.0 // Convert mm/min to mm/s
				}
			}
		}
	}

	// Add final layer
	if currentLayer != nil {
		result.Layers = append(result.Layers, *currentLayer)
	}

	// Calculate statistics
	result.LayerCount = len(result.Layers)
	result.EstimatedWeight = (result.TotalVolume / 1000) * matProps.Density // Convert mm³ to cm³

	// Calculate print time (simplified)
	for _, segment := range result.ExtrusionPath {
		if !segment.IsTravel {
			pathLength := segment.Start.Distance(segment.End)
			result.PrintTime += time.Duration(pathLength/segment.Speed) * time.Second
		}
	}

	p.log.Infof("FullControl simulation complete: %d layers, %.2f minutes print time, %.1fg material",
		result.LayerCount, result.PrintTime.Minutes(), result.EstimatedWeight)

	return result, nil
}

// parseMetadata extracts FullControl metadata from comments
func (p *FullControlGCodeParser) parseMetadata(line string, result *FullControlSimulationResult) {
	line = strings.TrimPrefix(line, ";")
	line = strings.TrimSpace(line)

	// Parse FullControl-specific metadata
	if strings.HasPrefix(line, "FullControl:") {
		// Extract FullControl parameters
		params := strings.TrimPrefix(line, "FullControl:")
		// Parse JSON-like parameters if present
		p.log.Debugf("FullControl metadata: %s", params)
	}

	// Common slicer metadata
	if strings.Contains(line, "Layer height:") {
		// Extract layer height
	} else if strings.Contains(line, "Print time:") {
		// Extract estimated print time
	} else if strings.Contains(line, "Filament used:") {
		// Extract filament usage
	}
}

// GenerateVisualizationData creates data optimized for Three.js visualization
func (p *FullControlGCodeParser) GenerateVisualizationData(result *FullControlSimulationResult) map[string]interface{} {
	visualization := make(map[string]interface{})

	// Convert extrusion path to Three.js line segments
	pathPoints := make([][]float64, 0)
	colors := make([][]float64, 0)

	for _, segment := range result.ExtrusionPath {
		// Add start and end points
		pathPoints = append(pathPoints,
			[]float64{segment.Start.X, segment.Start.Y, segment.Start.Z},
			[]float64{segment.End.X, segment.End.Y, segment.End.Z},
		)

		// Color based on segment type
		var color []float64
		if segment.IsRetraction {
			color = []float64{1.0, 0.0, 0.0} // Red for retractions
		} else if segment.IsTravel {
			color = []float64{0.5, 0.5, 0.5} // Gray for travel
		} else if segment.IsPrime {
			color = []float64{0.0, 0.0, 1.0} // Blue for prime
		} else {
			// Green gradient based on extrusion rate
			intensity := math.Min(segment.ExtrusionRate/10.0, 1.0)
			color = []float64{0.0, intensity, 1.0 - intensity}
		}

		colors = append(colors, color, color) // Same color for both points
	}

	visualization["path_points"] = pathPoints
	visualization["path_colors"] = colors
	visualization["layers"] = result.Layers
	visualization["bounding_box"] = result.BoundingBox
	visualization["material"] = result.MaterialType
	visualization["stats"] = map[string]interface{}{
		"layer_count":     result.LayerCount,
		"print_time_min":  result.PrintTime.Minutes(),
		"filament_meters": result.TotalFilament / 1000,
		"weight_grams":    result.EstimatedWeight,
		"volume_cm3":      result.TotalVolume / 1000,
	}

	return visualization
}

// StreamGCodeExecution simulates real-time GCODE execution for live visualization
func (p *FullControlGCodeParser) StreamGCodeExecution(gcode string, speedMultiplier float64) <-chan ExtrusionSegment {
	ch := make(chan ExtrusionSegment, 100)

	go func() {
		defer close(ch)

		// Parse and simulate
		result, err := p.SimulateFullControlGCode(gcode, "PLA", 0.4)
		if err != nil {
			p.log.Errorf("Simulation error: %v", err)
			return
		}

		// Stream segments with timing
		for _, segment := range result.ExtrusionPath {
			// Calculate segment duration
			pathLength := segment.Start.Distance(segment.End)
			duration := time.Duration(pathLength/segment.Speed*float64(time.Second)) / time.Duration(speedMultiplier)

			// Send segment
			ch <- segment

			// Simulate execution time
			time.Sleep(duration)
		}
	}()

	return ch
}