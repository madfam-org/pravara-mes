// Package matchmaking ranks producer machines for a product (MES-1 §5).
//
// Inputs: the product's RequirementProfile (from its type shell in
// asset-shells), each machine's capabilities (registry, overlaid by what the
// device declared in its Sparkplug DBIRTH), and its live state (State/Status,
// loaded material classes). Output: every candidate with the checks that
// passed or failed, eligible candidates ranked first. Nothing here computes
// geometry: a part's bounding box is compared only when a producer of it
// (yantra4d, a quote) supplied one.
package matchmaking

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Live device states (MES-1 §1 State/Status).
const (
	StateIdle     = "idle"
	StatePrinting = "printing"
	StatePaused   = "paused"
	StateError    = "error"
	StateOffline  = "offline"
)

// MaterialSlot is one Materials/Slot<n> pair from DBIRTH/DDATA.
type MaterialSlot struct {
	Slot   int    `json:"slot"`
	Class  string `json:"class,omitempty"` // a material-classes key; "" = unknown
	Loaded bool   `json:"loaded"`
}

// LiveState is what the Sparkplug primary host knows about a device.
type LiveState struct {
	// Born is true once a DBIRTH was received for the device in the current
	// node session (and no DDEATH/NDEATH since).
	Born   bool   `json:"born"`
	Status string `json:"status"`
	// Materials are the device's slots, in slot order.
	Materials []MaterialSlot `json:"materials,omitempty"`
	// Capabilities are the Capabilities/<key> metrics, keyed by the
	// fabrication-capabilities key.
	Capabilities map[string]any `json:"capabilities,omitempty"`
	ObservedAt   *time.Time     `json:"observed_at,omitempty"`
}

// LiveStateReader reads live device state. The Sparkplug primary host
// (telemetry-worker) persists it; pravara-api reads it through this
// interface so matchmaking does not depend on how it is stored.
type LiveStateReader interface {
	LiveStates(ctx context.Context, tenantID uuid.UUID, machineIDs []uuid.UUID) (map[uuid.UUID]LiveState, error)
}

// Machine is a registered machine as matchmaking sees it.
type Machine struct {
	ID             uuid.UUID `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	RegistryStatus string    `json:"registry_status"`
	// Capabilities are the operator-declared fabrication-capabilities values
	// (machines.specifications.fabrication_capabilities).
	Capabilities map[string]any `json:"capabilities,omitempty"`
	// PrinterProfile and Target name the fabrication-prep printer profile and
	// slice target for this machine (machines.metadata.fabrication_prep).
	PrinterProfile string `json:"printer_profile,omitempty"`
	Target         string `json:"target,omitempty"`
	// MaterialLots maps a slot number to the lot an operator recorded for the
	// spool in it (machines.metadata.material_lots). Sparkplug carries no lot.
	MaterialLots map[int]string `json:"material_lots,omitempty"`
	ActiveTasks  int            `json:"active_tasks"`
	// ReservedBy is the dispatch holding an active reservation, if any.
	ReservedBy *uuid.UUID `json:"reserved_by,omitempty"`
}

// SlicingProfile is one fabrication-prep catalog entry.
type SlicingProfile struct {
	Ref                string   `json:"ref"`
	ID                 string   `json:"id"`
	Kind               string   `json:"kind"`
	Target             string   `json:"target,omitempty"`
	MaterialClass      string   `json:"material_class,omitempty"`
	RequiresProcessTag string   `json:"requires_process_tag,omitempty"`
	Printers           []string `json:"printers,omitempty"`
	Tags               []string `json:"tags,omitempty"`
}

// Check results.
const (
	Pass        = "pass"
	Fail        = "fail"
	Unknown     = "unknown"
	NotRequired = "not_required"
)

// Check is one explained criterion.
type Check struct {
	Name   string `json:"check"`
	Result string `json:"result"`
	Detail string `json:"detail"`
}

// Selection is what dispatch needs from an eligible candidate.
type Selection struct {
	MaterialClass   string `json:"material_class,omitempty"`
	MaterialSlot    int    `json:"material_slot,omitempty"`
	MaterialLot     string `json:"material_lot,omitempty"`
	PrinterProfile  string `json:"printer_profile,omitempty"`
	FilamentProfile string `json:"filament_profile,omitempty"`
	ProcessProfile  string `json:"process_profile,omitempty"`
	Target          string `json:"target,omitempty"`
}

// Candidate is one machine with its explanation.
type Candidate struct {
	MachineID   uuid.UUID `json:"machine_id"`
	MachineCode string    `json:"machine_code"`
	MachineName string    `json:"machine_name"`
	Eligible    bool      `json:"eligible"`
	// Rank is 1-based among eligible candidates; 0 when not eligible.
	Rank                int               `json:"rank"`
	Checks              []Check           `json:"checks"`
	Selection           Selection         `json:"selection"`
	CapabilitySources   map[string]string `json:"capability_sources,omitempty"`
	CapabilityConflicts []string          `json:"capability_conflicts,omitempty"`
	ActiveTasks         int               `json:"active_tasks"`
}

// Result is a full match.
type Result struct {
	Candidates []Candidate `json:"candidates"`
	// Gaps name inputs that were not available, so a check could not run.
	Gaps []string `json:"gaps,omitempty"`
}

// Best returns the top-ranked eligible candidate, or nil.
func (r *Result) Best() *Candidate {
	for i := range r.Candidates {
		if r.Candidates[i].Eligible && r.Candidates[i].Rank == 1 {
			return &r.Candidates[i]
		}
	}
	return nil
}

// Gap identifiers.
const (
	GapBoundingBox     = "bounding_box_unavailable"
	GapSlicingCatalog  = "slicing_profile_catalog_unavailable"
	GapLiveState       = "live_state_unavailable"
	GapNoRequirements  = "requirement_profile_empty"
	GapMaterialLot     = "material_lot_not_recorded"
	GapPrinterTime     = "printer_reported_time_unavailable"
	GapBrokerTime      = "broker_receipt_time_unavailable"
	GapGcodeDigest     = "gcode_sha256_unavailable"
	GapIncompleteGOC1  = "goc1_inputs_incomplete"
	GapNozzleNotStated = "nozzle_requirement_not_stated"
)
