package dispatch

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
)

func at(s string) *time.Time {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		panic(err)
	}
	return &t
}

func TestConsumptionFacts_EstimatesAndMachineTime(t *testing.T) {
	slice := &SliceRecord{Estimates: map[string]any{"print_time_s": 975.0, "filament_g": 5.2, "source": "gcode"}}
	rec := &fabrication.ManufacturingRecord{MaterialClass: "tpu-95a", MaterialLot: "LOT-1",
		PrinterReportedAt: at("2026-10-04T10:00:00Z"), ServerRecordedAt: *at("2026-10-04T10:00:02Z")}

	facts := consumptionFacts(slice, rec, "FIL-TPU", at("2026-10-04T09:43:00Z"), at("2026-10-04T10:00:01Z"))
	require.Len(t, facts, 3)

	mat := facts[0]
	assert.Equal(t, repositories.ConsumptionMaterial, mat.Kind)
	assert.Equal(t, "FIL-TPU", mat.MaterialCode)
	assert.Equal(t, "LOT-1", mat.MaterialLot)
	assert.Equal(t, 5.2, mat.Quantity)
	assert.Equal(t, repositories.UnitGram, mat.Unit)
	assert.Equal(t, repositories.BasisEstimate, mat.Basis)
	assert.Equal(t, repositories.SourceSlicer, mat.Source)
	assert.Equal(t, "fabrication-prep estimate (gcode)", mat.Note)

	est := facts[1]
	assert.Equal(t, repositories.ConsumptionMachineTime, est.Kind)
	assert.InDelta(t, 16.25, est.Quantity, 1e-9)
	assert.Equal(t, repositories.BasisEstimate, est.Basis)

	act := facts[2]
	assert.Equal(t, repositories.ConsumptionMachineTime, act.Kind)
	assert.InDelta(t, 17.0, act.Quantity, 1e-9, "acknowledged 09:43 → printer reported 10:00")
	assert.Equal(t, repositories.BasisActual, act.Basis)
	assert.Equal(t, repositories.SourcePrinter, act.Source)

	for _, f := range facts {
		assert.Equal(t, *rec.PrinterReportedAt, f.OccurredAt, "occurred_at is the printer's completion")
		f.TenantID, f.JobKind, f.JobID = uuid.New(), repositories.JobKindDispatch, uuid.New()
		assert.NoError(t, f.Validate())
	}
}

func TestConsumptionFacts_MissingFacts(t *testing.T) {
	rec := &fabrication.ManufacturingRecord{MaterialClass: "pla", ServerRecordedAt: *at("2026-10-04T10:00:02Z")}

	// No estimates, no acknowledgement: nothing to record.
	assert.Empty(t, consumptionFacts(&SliceRecord{}, rec, "pla", nil, nil))
	assert.Empty(t, consumptionFacts(nil, rec, "pla", nil, nil))

	// No material code: machine time only.
	slice := &SliceRecord{Estimates: map[string]any{"print_time_s": json.Number("60"), "filament_g": 3.0}}
	facts := consumptionFacts(slice, rec, "", nil, nil)
	require.Len(t, facts, 1)
	assert.Equal(t, repositories.ConsumptionMachineTime, facts[0].Kind)
	assert.InDelta(t, 1.0, facts[0].Quantity, 1e-9)

	// Without a printer time, the ledger completion ends the job; an
	// acknowledgement after the end records no actual.
	facts = consumptionFacts(nil, rec, "pla", at("2026-10-04T09:00:00Z"), at("2026-10-04T09:30:00Z"))
	require.Len(t, facts, 1)
	assert.InDelta(t, 30.0, facts[0].Quantity, 1e-9)
	assert.Equal(t, *at("2026-10-04T09:30:00Z"), facts[0].OccurredAt)
	assert.Empty(t, consumptionFacts(nil, rec, "pla", at("2026-10-04T11:00:00Z"), nil))

	// Negative or non-numeric estimates are ignored.
	slice = &SliceRecord{Estimates: map[string]any{"print_time_s": "n/a", "filament_g": -1.0}}
	assert.Empty(t, consumptionFacts(slice, rec, "pla", nil, nil))
}
