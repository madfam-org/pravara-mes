package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/expr-lang/expr"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
)

// HyperobjectMapper transforms a Yantra4D manifest into PravaraMES domain objects.
type HyperobjectMapper struct {
	productRepo *repositories.ProductRepository
	wiRepo      *repositories.WorkInstructionRepository
	publisher   *pubsub.Publisher
	log         *logrus.Logger
}

// NewHyperobjectMapper creates a new mapper.
func NewHyperobjectMapper(
	productRepo *repositories.ProductRepository,
	wiRepo *repositories.WorkInstructionRepository,
	publisher *pubsub.Publisher,
	log *logrus.Logger,
) *HyperobjectMapper {
	return &HyperobjectMapper{
		productRepo: productRepo,
		wiRepo:      wiRepo,
		publisher:   publisher,
		log:         log,
	}
}

// HyperobjectImportRequest is the input for an import operation.
type HyperobjectImportRequest struct {
	TenantID    uuid.UUID              `json:"tenant_id"`
	Manifest    *Yantra4DManifest      `json:"manifest"`
	Params      map[string]interface{} `json:"params"`
	Mode        string                 `json:"mode"`
	GLBModelURL string                 `json:"glb_model_url"`
}

// HyperobjectImportResult holds the created domain objects.
type HyperobjectImportResult struct {
	ProductDefinition *repositories.ProductDefinition `json:"product_definition"`
	BOMItems          []repositories.BOMItem          `json:"bom_items"`
	WorkInstruction   *repositories.WorkInstruction    `json:"work_instruction,omitempty"`
}

// Yantra4DManifest mirrors the manifest structure from the viz-engine yantra4d client.
// Duplicated here to avoid cross-app imports; both are derived from the same Yantra4D API.
type Yantra4DManifest struct {
	Project struct {
		Name        string            `json:"name"`
		Slug        string            `json:"slug"`
		Version     string            `json:"version"`
		Description map[string]string `json:"description"`
		Tags        []string          `json:"tags"`
		Difficulty  string            `json:"difficulty"`
		Engine      string            `json:"engine"`
	} `json:"project"`
	Modes []struct {
		ID    string            `json:"id"`
		Label map[string]string `json:"label"`
	} `json:"modes"`
	Parameters []struct {
		ID      string      `json:"id"`
		Type    string      `json:"type"`
		Default interface{} `json:"default"`
		Label   map[string]string `json:"label"`
		Group   string      `json:"group"`
	} `json:"parameters"`
	BOM struct {
		Hardware []struct {
			ID              string            `json:"id"`
			Label           map[string]string `json:"label"`
			QuantityFormula string            `json:"quantity_formula"`
			Unit            string            `json:"unit"`
			SupplierURL     string            `json:"supplier_url"`
		} `json:"hardware"`
	} `json:"bom"`
	AssemblySteps []struct {
		Step           int               `json:"step"`
		Label          map[string]string `json:"label"`
		Notes          map[string]string `json:"notes"`
		VisibleParts   []string          `json:"visible_parts"`
		HighlightParts []string          `json:"highlight_parts"`
		Camera         []float64         `json:"camera"`
		CameraTarget   []float64         `json:"camera_target"`
		Hardware       []string          `json:"hardware"`
	} `json:"assembly_steps"`
	Hyperobject struct {
		IsHyperobject bool   `json:"is_hyperobject"`
		Domain        string `json:"domain"`
	} `json:"hyperobject"`
	Verification struct {
		Geometry struct {
			Watertight bool       `json:"watertight"`
			BodyCount  int        `json:"body_count"`
			Dimensions [3]float64 `json:"dimensions"`
			FacetCount int        `json:"facet_count"`
		} `json:"geometry"`
		Printability struct {
			ThinWall       float64 `json:"thin_wall"`
			Overhang       float64 `json:"overhang"`
			MinFeatureSize float64 `json:"min_feature_size"`
		} `json:"printability"`
	} `json:"verification"`
}

// Import creates all MES domain objects from a Yantra4D manifest.
func (m *HyperobjectMapper) Import(ctx context.Context, req HyperobjectImportRequest) (*HyperobjectImportResult, error) {
	manifest := req.Manifest
	if manifest == nil {
		return nil, fmt.Errorf("manifest is required")
	}

	m.log.WithFields(logrus.Fields{
		"slug":      manifest.Project.Slug,
		"tenant_id": req.TenantID,
	}).Info("Starting hyperobject domain mapping")

	// 1. Create product definition
	mode := req.Mode
	if mode == "" && len(manifest.Modes) > 0 {
		mode = manifest.Modes[0].ID
	}

	sku := fmt.Sprintf("Y4D-%s-%s", manifest.Project.Slug, mode)
	category := InferCategoryFromEngine(manifest.Project.Engine)
	description := manifest.Project.Description["en"]

	// Build parametric specs from params + parameter definitions
	parametricSpecs := make(map[string]interface{})
	for _, p := range manifest.Parameters {
		val, ok := req.Params[p.ID]
		if !ok {
			val = p.Default
		}
		parametricSpecs[p.ID] = map[string]interface{}{
			"value": val,
			"type":  p.Type,
			"label": p.Label["en"],
			"group": p.Group,
		}
	}

	metadata := map[string]interface{}{
		"source":     "yantra4d",
		"slug":       manifest.Project.Slug,
		"tags":       manifest.Project.Tags,
		"domain":     manifest.Hyperobject.Domain,
		"engine":     manifest.Project.Engine,
		"difficulty": manifest.Project.Difficulty,
	}

	product := &repositories.ProductDefinition{
		TenantID:        req.TenantID,
		SKU:             sku,
		Name:            manifest.Project.Name,
		Version:         manifest.Project.Version,
		Category:        category,
		Description:     description,
		CADFileURL:      req.GLBModelURL,
		ParametricSpecs: parametricSpecs,
		IsActive:        true,
		Metadata:        metadata,
	}

	if err := m.productRepo.Create(ctx, product); err != nil {
		return nil, fmt.Errorf("create product definition: %w", err)
	}

	m.log.WithField("product_id", product.ID).Info("Product definition created")

	// 2. Create BOM items from hardware list
	bomItems, err := m.createBOMItems(ctx, req.TenantID, product.ID, manifest, req.Params)
	if err != nil {
		m.log.WithError(err).Warn("Failed to create some BOM items")
	}

	// 3. Create work instruction from assembly steps
	var wi *repositories.WorkInstruction
	if len(manifest.AssemblySteps) > 0 {
		wi, err = m.createWorkInstruction(ctx, req.TenantID, product, manifest)
		if err != nil {
			m.log.WithError(err).Warn("Failed to create work instruction")
		}
	}

	// 4. Publish event
	if m.publisher != nil {
		event := pubsub.NewEvent(pubsub.EventProductImported, req.TenantID, pubsub.EntityCreatedData{
			EntityID:   product.ID,
			EntityType: "product_definition",
			Name:       product.Name,
			CreatedBy:  uuid.Nil,
			CreatedAt:  time.Now().UTC(),
			Metadata: map[string]interface{}{
				"source":  "yantra4d",
				"slug":    manifest.Project.Slug,
				"sku":     sku,
				"bom_count": len(bomItems),
				"has_work_instruction": wi != nil,
			},
		})
		if err := m.publisher.Publish(ctx, pubsub.NamespaceProducts, req.TenantID, event); err != nil {
			m.log.WithError(err).Warn("Failed to publish product imported event")
		}
	}

	return &HyperobjectImportResult{
		ProductDefinition: product,
		BOMItems:          bomItems,
		WorkInstruction:   wi,
	}, nil
}

// createBOMItems evaluates quantity formulas and creates BOM items.
func (m *HyperobjectMapper) createBOMItems(ctx context.Context, tenantID, productID uuid.UUID, manifest *Yantra4DManifest, params map[string]interface{}) ([]repositories.BOMItem, error) {
	var items []repositories.BOMItem

	for i, hw := range manifest.BOM.Hardware {
		quantity, err := evaluateFormula(hw.QuantityFormula, params)
		if err != nil {
			m.log.WithFields(logrus.Fields{
				"hardware_id": hw.ID,
				"formula":     hw.QuantityFormula,
			}).WithError(err).Warn("Failed to evaluate BOM quantity formula, using 1.0")
			quantity = 1.0
		}

		// Skip zero-quantity items (e.g. conditional BOM entries)
		if quantity <= 0 {
			continue
		}

		label := hw.Label["en"]
		if label == "" {
			label = hw.ID
		}

		item := &repositories.BOMItem{
			TenantID:            tenantID,
			ProductDefinitionID: productID,
			MaterialName:        label,
			MaterialCode:        hw.ID,
			Quantity:            quantity,
			Unit:                hw.Unit,
			Supplier:            hw.SupplierURL,
			SortOrder:           i,
		}

		if err := m.productRepo.CreateBOMItem(ctx, item); err != nil {
			m.log.WithError(err).WithField("hardware_id", hw.ID).Warn("Failed to create BOM item")
			continue
		}

		items = append(items, *item)
	}

	m.log.WithField("count", len(items)).Info("BOM items created")
	return items, nil
}

// createWorkInstruction builds a work instruction from assembly steps.
func (m *HyperobjectMapper) createWorkInstruction(ctx context.Context, tenantID uuid.UUID, product *repositories.ProductDefinition, manifest *Yantra4DManifest) (*repositories.WorkInstruction, error) {
	// Build steps JSONB
	type stepMedia struct {
		Camera         []float64 `json:"camera,omitempty"`
		CameraTarget   []float64 `json:"camera_target,omitempty"`
		VisibleParts   []string  `json:"visible_parts,omitempty"`
		HighlightParts []string  `json:"highlight_parts,omitempty"`
		HardwareIDs    []string  `json:"hardware_ids,omitempty"`
	}

	type wiStep struct {
		Number      int       `json:"number"`
		Title       string    `json:"title"`
		Description string    `json:"description"`
		Media       stepMedia `json:"media"`
	}

	var steps []wiStep
	for _, as := range manifest.AssemblySteps {
		title := as.Label["en"]
		if title == "" {
			title = fmt.Sprintf("Step %d", as.Step)
		}
		notes := as.Notes["en"]

		steps = append(steps, wiStep{
			Number:      as.Step,
			Title:       title,
			Description: notes,
			Media: stepMedia{
				Camera:         as.Camera,
				CameraTarget:   as.CameraTarget,
				VisibleParts:   as.VisibleParts,
				HighlightParts: as.HighlightParts,
				HardwareIDs:    as.Hardware,
			},
		})
	}

	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		return nil, fmt.Errorf("marshal steps: %w", err)
	}

	wi := &repositories.WorkInstruction{
		TenantID:            tenantID,
		Title:               fmt.Sprintf("Assembly: %s", product.Name),
		Version:             product.Version,
		Category:            "operation",
		Description:         fmt.Sprintf("Assembly instructions for %s imported from Yantra4D", product.Name),
		ProductDefinitionID: &product.ID,
		Steps:               stepsJSON,
		IsActive:            true,
		Metadata: map[string]interface{}{
			"source": "yantra4d",
			"slug":   manifest.Project.Slug,
		},
	}

	if err := m.wiRepo.Create(ctx, wi); err != nil {
		return nil, fmt.Errorf("create work instruction: %w", err)
	}

	m.log.WithFields(logrus.Fields{
		"wi_id":      wi.ID,
		"step_count": len(steps),
	}).Info("Work instruction created")

	return wi, nil
}

// evaluateFormula safely evaluates a BOM quantity expression using expr-lang.
func evaluateFormula(formula string, params map[string]interface{}) (float64, error) {
	if formula == "" {
		return 1.0, nil
	}

	// Build environment with parameter values
	env := make(map[string]interface{})
	for k, v := range params {
		env[k] = v
	}

	program, err := expr.Compile(formula, expr.Env(env))
	if err != nil {
		return 0, fmt.Errorf("compile formula %q: %w", formula, err)
	}

	result, err := expr.Run(program, env)
	if err != nil {
		return 0, fmt.Errorf("evaluate formula %q: %w", formula, err)
	}

	switch v := result.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case bool:
		if v {
			return 1.0, nil
		}
		return 0.0, nil
	default:
		return 0, fmt.Errorf("formula %q returned non-numeric type %T", formula, result)
	}
}

// InferCategoryFromEngine maps Yantra4D engine to a PravaraMES product category.
func InferCategoryFromEngine(engine string) string {
	switch engine {
	case "openscad", "scad":
		return "3d_print"
	case "cadquery", "cq":
		return "cnc_part"
	case "freecad":
		return "cnc_part"
	default:
		return "3d_print"
	}
}
