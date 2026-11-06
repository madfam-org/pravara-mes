package repositories

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestJobConsumptionValidate(t *testing.T) {
	ok := func() JobConsumption {
		return JobConsumption{JobKind: JobKindDispatch, JobID: uuid.New(), Kind: ConsumptionMaterial,
			MaterialCode: "FIL-PLA", Quantity: 5.2, Unit: UnitGram, Basis: BasisEstimate, Source: SourceSlicer,
			OccurredAt: time.Now()}
	}
	base := ok()
	assert.NoError(t, base.Validate())
	minutes := JobConsumption{JobKind: JobKindTask, JobID: uuid.New(), Kind: ConsumptionMachineTime, Quantity: 0,
		Unit: UnitMinute, Basis: BasisActual, Source: SourceOperator, OccurredAt: time.Now()}
	assert.NoError(t, minutes.Validate())

	for name, mutate := range map[string]func(*JobConsumption){
		"job kind":             func(c *JobConsumption) { c.JobKind = "order" },
		"job id":               func(c *JobConsumption) { c.JobID = uuid.Nil },
		"basis":                func(c *JobConsumption) { c.Basis = "standard" },
		"source":               func(c *JobConsumption) { c.Source = "forgesight" },
		"negative quantity":    func(c *JobConsumption) { c.Quantity = -1 },
		"NaN quantity":         func(c *JobConsumption) { c.Quantity = math.NaN() },
		"huge quantity":        func(c *JobConsumption) { c.Quantity = 1e12 },
		"material unit":        func(c *JobConsumption) { c.Unit = "kg" },
		"material in minutes":  func(c *JobConsumption) { c.Unit = UnitMinute },
		"material code":        func(c *JobConsumption) { c.MaterialCode = "" },
		"kind":                 func(c *JobConsumption) { c.Kind = "labor" },
		"occurred_at":          func(c *JobConsumption) { c.OccurredAt = time.Time{} },
		"machine time in g":    func(c *JobConsumption) { c.Kind = ConsumptionMachineTime },
		"machine time w/ code": func(c *JobConsumption) { c.Kind, c.Unit = ConsumptionMachineTime, UnitMinute },
	} {
		c := ok()
		mutate(&c)
		assert.Error(t, c.Validate(), name)
	}
}

func TestRound4(t *testing.T) {
	assert.Equal(t, 17.0042, Round4(17.004166666))
	assert.Equal(t, 0.8, Round4(6-5.2))
}
