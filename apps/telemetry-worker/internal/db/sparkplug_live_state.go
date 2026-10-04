package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/packages/sparkplug"
	"github.com/madfam-org/pravara-mes/packages/sparkplug/host"
)

// MaterialSlot is one entry of machine_live_state.material_slots.
type MaterialSlot struct {
	Slot   int     `json:"slot"`
	Class  *string `json:"class"` // a material-classes key; null when unknown
	Loaded bool    `json:"loaded"`
}

// liveState is one machine_live_state row.
type liveState struct {
	exists         bool
	online         bool
	stateStatus    sql.NullString
	progress       sql.NullFloat64
	hotend, bed    sql.NullFloat64
	slots          map[int]*MaterialSlot
	capabilities   map[string]any
	properties     map[string]any
	jobID          sql.NullString
	jobStatus      sql.NullString
	commandLastID  sql.NullString
	commandStatus  sql.NullString
	commandError   sql.NullString
	bdSeq          sql.NullInt64
	bornAt, diedAt sql.NullTime
	reportedAt     sql.NullTime
}

func emptyLiveState() *liveState {
	return &liveState{slots: map[int]*MaterialSlot{}, capabilities: map[string]any{}, properties: map[string]any{}}
}

var (
	validStateStatus   = set("idle", "printing", "paused", "error", "offline")
	validJobStatus     = set("queued", "printing", "complete", "failed", "cancelled")
	validCommandStatus = set("accepted", "running", "done", "failed")
)

func set(vs ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range vs {
		m[v] = true
	}
	return m
}

// loadLiveState reads and locks the row of machineID.
func loadLiveState(ctx context.Context, tx *sql.Tx, tenantID, machineID uuid.UUID) (*liveState, error) {
	ls := emptyLiveState()
	var slots, caps, props []byte
	err := tx.QueryRowContext(ctx, `
		SELECT online, state_status, progress, hotend_temp_c, bed_temp_c, material_slots,
		       capabilities, properties, job_id, job_status, command_last_id, command_status,
		       command_error, bdseq, born_at, died_at, reported_at
		FROM machine_live_state
		WHERE machine_id = $1 AND tenant_id = $2
		FOR UPDATE`, machineID, tenantID,
	).Scan(&ls.online, &ls.stateStatus, &ls.progress, &ls.hotend, &ls.bed, &slots, &caps, &props,
		&ls.jobID, &ls.jobStatus, &ls.commandLastID, &ls.commandStatus, &ls.commandError,
		&ls.bdSeq, &ls.bornAt, &ls.diedAt, &ls.reportedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return ls, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load live state: %w", err)
	}
	ls.exists = true
	var list []MaterialSlot
	if err := json.Unmarshal(slots, &list); err != nil {
		return nil, fmt.Errorf("live state material slots: %w", err)
	}
	for i := range list {
		s := list[i]
		ls.slots[s.Slot] = &s
	}
	if err := json.Unmarshal(caps, &ls.capabilities); err != nil {
		return nil, fmt.Errorf("live state capabilities: %w", err)
	}
	if err := json.Unmarshal(props, &ls.properties); err != nil {
		return nil, fmt.Errorf("live state properties: %w", err)
	}
	return ls, nil
}

// apply merges a report's values. A birth replaces the device's state.
// Values outside the MES-1 value sets are skipped and returned as warnings.
func (ls *liveState) apply(r host.DeviceReport) []string {
	var warnings []string
	if r.Birth {
		prev := ls
		*ls = *emptyLiveState()
		ls.exists = prev.exists
		ls.bornAt = sql.NullTime{Time: r.Timestamp, Valid: true}
		ls.online = true
	}
	ls.bdSeq = sql.NullInt64{Int64: int64(r.BdSeq), Valid: true}
	ls.reportedAt = sql.NullTime{Time: r.Timestamp, Valid: true}

	str := func(v any) (sql.NullString, bool) {
		if v == nil {
			return sql.NullString{}, true
		}
		s, ok := v.(string)
		return sql.NullString{String: s, Valid: ok}, ok
	}
	num := func(v any) (sql.NullFloat64, bool) {
		switch x := v.(type) {
		case nil:
			return sql.NullFloat64{}, true
		case float64:
			return sql.NullFloat64{Float64: x, Valid: true}, true
		case float32:
			return sql.NullFloat64{Float64: float64(x), Valid: true}, true
		case int64:
			return sql.NullFloat64{Float64: float64(x), Valid: true}, true
		case uint64:
			return sql.NullFloat64{Float64: float64(x), Valid: true}, true
		}
		return sql.NullFloat64{}, false
	}
	enum := func(name sparkplug.MetricName, v any, allowed map[string]bool, dst *sql.NullString) {
		s, ok := str(v)
		if !ok || (s.Valid && !allowed[s.String]) {
			warnings = append(warnings, fmt.Sprintf("%s value %v is outside the MES-1 value set", name, v))
			return
		}
		*dst = s
	}

	for name, v := range r.Values {
		switch name {
		case sparkplug.MetricStateStatus:
			enum(name, v, validStateStatus, &ls.stateStatus)
		case sparkplug.MetricJobStatus:
			enum(name, v, validJobStatus, &ls.jobStatus)
		case sparkplug.MetricCommandStatus:
			enum(name, v, validCommandStatus, &ls.commandStatus)
		case sparkplug.MetricStateProgress:
			if f, ok := num(v); ok && (!f.Valid || (f.Float64 >= 0 && f.Float64 <= 100)) {
				ls.progress = f
			} else {
				warnings = append(warnings, fmt.Sprintf("%s value %v is outside 0..100", name, v))
			}
		case sparkplug.MetricTempsHotend:
			if f, ok := num(v); ok {
				ls.hotend = f
			}
		case sparkplug.MetricTempsBed:
			if f, ok := num(v); ok {
				ls.bed = f
			}
		case sparkplug.MetricJobID:
			ls.jobID, _ = str(v)
		case sparkplug.MetricCommandLastID:
			ls.commandLastID, _ = str(v)
		case sparkplug.MetricCommandError:
			ls.commandError, _ = str(v)
		case sparkplug.MetricPropertiesModel:
			ls.properties["model"] = v
		case sparkplug.MetricPropertiesFirmware:
			ls.properties["firmware"] = v
		case sparkplug.MetricPropertiesConnectivity:
			ls.properties["connectivity"] = v
		default:
			if key, ok := strings.CutPrefix(string(name), sparkplug.CapabilitiesPrefix); ok && key != "" {
				ls.capabilities[key] = v
				continue
			}
			if slot, field, ok := sparkplug.ParseMaterialSlotMetric(name); ok {
				s := ls.slots[slot]
				if s == nil {
					s = &MaterialSlot{Slot: slot}
					ls.slots[slot] = s
				}
				switch field {
				case "Class":
					if c, ok := v.(string); ok {
						s.Class = &c
					} else {
						s.Class = nil
					}
				case "Loaded":
					b, _ := v.(bool)
					s.Loaded = b
				}
			}
			// Command inputs (Command/Id, ...) declared in DBIRTH carry no state.
		}
	}
	return warnings
}

// save upserts the row.
func (ls *liveState) save(ctx context.Context, tx *sql.Tx, tenantID, machineID uuid.UUID, edgeNodeID string, now time.Time) error {
	slots := make([]MaterialSlot, 0, len(ls.slots))
	for _, s := range ls.slots {
		slots = append(slots, *s)
	}
	sort.Slice(slots, func(i, j int) bool { return slots[i].Slot < slots[j].Slot })
	slotJSON, err := json.Marshal(slots)
	if err != nil {
		return err
	}
	capJSON, err := json.Marshal(ls.capabilities)
	if err != nil {
		return err
	}
	propJSON, err := json.Marshal(ls.properties)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `
		INSERT INTO machine_live_state (
			machine_id, tenant_id, edge_node_id, online, state_status, progress, hotend_temp_c,
			bed_temp_c, material_slots, capabilities, properties, job_id, job_status,
			command_last_id, command_status, command_error, bdseq, born_at, died_at, reported_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21)
		ON CONFLICT (machine_id) DO UPDATE SET
			edge_node_id = EXCLUDED.edge_node_id, online = EXCLUDED.online,
			state_status = EXCLUDED.state_status, progress = EXCLUDED.progress,
			hotend_temp_c = EXCLUDED.hotend_temp_c, bed_temp_c = EXCLUDED.bed_temp_c,
			material_slots = EXCLUDED.material_slots, capabilities = EXCLUDED.capabilities,
			properties = EXCLUDED.properties, job_id = EXCLUDED.job_id, job_status = EXCLUDED.job_status,
			command_last_id = EXCLUDED.command_last_id, command_status = EXCLUDED.command_status,
			command_error = EXCLUDED.command_error, bdseq = EXCLUDED.bdseq, born_at = EXCLUDED.born_at,
			died_at = EXCLUDED.died_at, reported_at = EXCLUDED.reported_at, updated_at = EXCLUDED.updated_at`,
		machineID, tenantID, edgeNodeID, ls.online, ls.stateStatus, ls.progress, ls.hotend, ls.bed,
		slotJSON, capJSON, propJSON, ls.jobID, ls.jobStatus, ls.commandLastID, ls.commandStatus,
		ls.commandError, ls.bdSeq, ls.bornAt, ls.diedAt, ls.reportedAt, now)
	if err != nil {
		return fmt.Errorf("save live state: %w", err)
	}
	return nil
}
