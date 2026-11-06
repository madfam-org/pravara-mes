package services

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

func f64(v float64) *float64 { return &v }
func intp(v int) *int        { return &v }

func TestFilamentGrams(t *testing.T) {
	// 1 m of 1.75 mm PLA (1.24 g/cm³) is about 2.98 g.
	g, ok := FilamentGrams(1000, f64(1.75), f64(1.24))
	require.True(t, ok)
	assert.InDelta(t, 1000*math.Pi*0.875*0.875/1000*1.24, g, 1e-9)
	assert.InDelta(t, 2.98, g, 0.01)

	for _, tc := range []struct{ d, rho *float64 }{{nil, f64(1.24)}, {f64(1.75), nil}, {f64(0), f64(1.24)}, {f64(1.75), f64(-1)}} {
		_, ok := FilamentGrams(1000, tc.d, tc.rho)
		assert.False(t, ok)
	}
}

// validFor fills the job fields Record would set, so Validate can run.
func validFor(t *testing.T, facts []repositories.JobConsumption) {
	t.Helper()
	job := JobRef{TenantID: uuid.New(), Kind: repositories.JobKindPrintJob, ID: uuid.New()}
	for _, f := range facts {
		job.apply(&f)
		assert.NoError(t, f.Validate(), "%+v", f)
	}
}

func TestPrintJobFacts(t *testing.T) {
	end := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	start := end.Add(-90 * time.Minute)

	t.Run("profile with diameter and density converts mm to g", func(t *testing.T) {
		p := &repositories.PrintJobUsage{MaterialType: "PLA", MaterialDiameterMM: f64(1.75), MaterialDensityGcm3: f64(1.24),
			EstimatedFilamentG: f64(12.5), EstimatedFilamentMM: f64(4200), ActualFilamentMM: f64(4000),
			EstimatedSeconds: intp(3600), ActualSeconds: intp(3900), StartedAt: &start, CompletedAt: &end}
		facts := PrintJobFacts(p, end)
		require.Len(t, facts, 4)

		assert.Equal(t, repositories.UnitGram, facts[0].Unit)
		assert.Equal(t, 12.5, facts[0].Quantity, "the G-code's grams are used as is")
		assert.Equal(t, repositories.BasisEstimate, facts[0].Basis)
		assert.Equal(t, repositories.SourceSlicer, facts[0].Source)

		g, _ := FilamentGrams(4000, f64(1.75), f64(1.24))
		assert.Equal(t, repositories.UnitGram, facts[1].Unit)
		assert.InDelta(t, g, facts[1].Quantity, 1e-9)
		assert.Equal(t, repositories.BasisActual, facts[1].Basis)
		assert.Equal(t, repositories.SourcePrinter, facts[1].Source)
		assert.Contains(t, facts[1].Note, "converted from 4000 mm")

		assert.Equal(t, repositories.ConsumptionMachineTime, facts[2].Kind)
		assert.Equal(t, 60.0, facts[2].Quantity)
		assert.Equal(t, 65.0, facts[3].Quantity, "actual_time_seconds wins over the timestamps")
		for _, f := range facts {
			assert.True(t, f.OccurredAt.Equal(end))
			if f.Kind == repositories.ConsumptionMaterial {
				assert.Equal(t, "PLA", f.MaterialCode)
			}
		}
		validFor(t, facts)
	})

	t.Run("profile without density keeps mm and says so", func(t *testing.T) {
		p := &repositories.PrintJobUsage{MaterialType: "PETG", MaterialDiameterMM: f64(1.75),
			EstimatedFilamentMM: f64(4200), ActualFilamentMM: f64(4000), StartedAt: &start, CompletedAt: &end}
		facts := PrintJobFacts(p, end)
		require.Len(t, facts, 3)
		for _, f := range facts[:2] {
			assert.Equal(t, repositories.UnitMillimetre, f.Unit)
			assert.Contains(t, f.Note, keptInMM)
		}
		assert.Equal(t, 4200.0, facts[0].Quantity)
		assert.Equal(t, 4000.0, facts[1].Quantity)
		assert.Equal(t, 90.0, facts[2].Quantity, "started_at to completed_at without a reported print time")
		validFor(t, facts)
	})

	t.Run("no material profile records machine time only", func(t *testing.T) {
		p := &repositories.PrintJobUsage{EstimatedFilamentG: f64(3), EstimatedSeconds: intp(120)}
		facts := PrintJobFacts(p, end)
		require.Len(t, facts, 1)
		assert.Equal(t, repositories.ConsumptionMachineTime, facts[0].Kind)
		validFor(t, facts)
	})
}

func TestEffective(t *testing.T) {
	assert.Nil(t, effective(nil))
	est := repositories.JobConsumption{Basis: repositories.BasisEstimate, Quantity: 5}
	act1 := repositories.JobConsumption{Basis: repositories.BasisActual, Quantity: 6}
	act2 := repositories.JobConsumption{Basis: repositories.BasisActual, Quantity: 7}
	assert.Equal(t, 5.0, effective([]repositories.JobConsumption{est}).Quantity)
	assert.Equal(t, 6.0, effective([]repositories.JobConsumption{est, act1}).Quantity, "an actual wins")
	assert.Equal(t, 7.0, effective([]repositories.JobConsumption{act1, est, act2}).Quantity, "the latest actual wins")
}
