package fabrication

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

// ManufacturingRecordFormat identifies the record document pravara stores and
// publishes (MES-1 §7).
const ManufacturingRecordFormat = "pravara.manufacturing-record"

// ProfileDigest is one slicer profile with its content digest.
type ProfileDigest struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

// ManufacturingRecord is what pravara knows, as facts, about one dispatched
// and completed job. Absent facts are listed in Gaps instead of guessed.
type ManufacturingRecord struct {
	Format        string `json:"format"`
	FormatVersion string `json:"format_version"`

	DispatchID string `json:"dispatch_id"`
	CommandID  string `json:"command_id"`
	TaskID     string `json:"task_id"`

	// Design (GOC-1 render bundle).
	TypeShellID     string `json:"type_shell_id"`
	Cartridge       string `json:"cartridge"`
	Mode            string `json:"mode"`
	Part            string `json:"part,omitempty"`
	GOC1InstanceID  string `json:"goc1_instance_id"`
	VariablesSHA256 string `json:"variables_sha256"`
	TreeSHA256      string `json:"tree_sha256"`
	SidecarSHA256   string `json:"sidecar_sha256"`
	GeometrySHA256  string `json:"geometry_sha256"`
	GeometryMedia   string `json:"geometry_media_type"`

	// Slicing (fabrication-prep).
	SliceJobID            string                   `json:"slice_job_id"`
	SlicerProfiles        map[string]ProfileDigest `json:"slicer_profiles"`
	Slicer                string                   `json:"slicer,omitempty"`
	EffectiveSHA256       string                   `json:"effective_sha256,omitempty"`
	SlicerVariablesSHA256 string                   `json:"slicer_variables_sha256"`
	ArtifactSHA256        string                   `json:"artifact_sha256"`
	ArtifactMediaType     string                   `json:"artifact_media_type"`
	GcodeSHA256           string                   `json:"gcode_sha256,omitempty"`

	// Production.
	MachineID     string `json:"machine_id"`
	MachineCode   string `json:"machine_code"`
	MaterialClass string `json:"material_class"`
	MaterialSlot  int    `json:"material_slot,omitempty"`
	MaterialLot   string `json:"material_lot,omitempty"`

	// Timestamps of the completion: as reported by the printer (Sparkplug
	// metric timestamp), as received by the broker host, and as recorded by
	// pravara's database.
	PrinterReportedAt *time.Time `json:"printer_reported_at,omitempty"`
	BrokerReceivedAt  *time.Time `json:"broker_received_at,omitempty"`
	ServerRecordedAt  time.Time  `json:"server_recorded_at"`

	GenealogyID string   `json:"genealogy_id,omitempty"`
	Gaps        []string `json:"gaps,omitempty"`
}

// Digest returns the sha256 of the record's JSON encoding.
func (r *ManufacturingRecord) Digest() (string, []byte, error) {
	raw, err := json.Marshal(r)
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), raw, nil
}
