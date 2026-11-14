package services

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

func TestBuiltinConversionsAreExactAndNeverGuessed(t *testing.T) {
	for _, tc := range []struct {
		from, to string
		want     float64
	}{{"g", "g", 1}, {"kg", "g", 1000}, {"g", "kg", 0.001}, {"l", "ml", 1000}, {"m", "mm", 1000}, {"cm", "mm", 10}} {
		f, ok := BuiltinConversion(tc.from, tc.to)
		assert.True(t, ok, "%s→%s", tc.from, tc.to)
		assert.Equal(t, tc.want, f)
	}
	for _, pair := range [][2]string{{"spool", "g"}, {"mm", "g"}, {"sheet", "pcs"}, {"ml", "g"}} {
		_, ok := BuiltinConversion(pair[0], pair[1])
		assert.False(t, ok, "%v needs a per-material conversion", pair)
	}
}

func TestReceiptInputNeedsInvoiceEvidence(t *testing.T) {
	cfdi, line, zero := uuid.New(), 2, 0
	ok := ReceiptInput{MaterialID: uuid.New(), Quantity: 1, Unit: "kg", ReceivedAt: time.Now(),
		InvoiceStatus: repositories.ReceiptInvoiced, CFDIUUID: &cfdi, CFDILine: &line, IdempotencyKey: "synthetic-key"}
	assert.NoError(t, ok.Validate())
	noInvoice := ok
	noInvoice.InvoiceStatus, noInvoice.CFDIUUID, noInvoice.CFDILine = repositories.ReceiptNoInvoice, nil, nil
	assert.NoError(t, noInvoice.Validate())

	for name, mutate := range map[string]func(*ReceiptInput){
		"no status":         func(in *ReceiptInput) { in.InvoiceStatus = "" },
		"invoiced, no line": func(in *ReceiptInput) { in.CFDILine = nil },
		"invoiced, line 0":  func(in *ReceiptInput) { in.CFDILine = &zero },
		"no invoice + cfdi": func(in *ReceiptInput) { in.InvoiceStatus = repositories.ReceiptNoInvoice },
		"zero quantity":     func(in *ReceiptInput) { in.Quantity = 0 },
		"bad unit":          func(in *ReceiptInput) { in.Unit = "Kilo Grams" },
		"future":            func(in *ReceiptInput) { in.ReceivedAt = time.Now().Add(time.Hour) },
		"lot id and number": func(in *ReceiptInput) { id := uuid.New(); in.BatchLotID, in.LotNumber = &id, "LOT-1" },
		"no material":       func(in *ReceiptInput) { in.MaterialID = uuid.Nil },
	} {
		in := ok
		mutate(&in)
		err := in.Validate()
		assert.True(t, errors.Is(err, ErrReceiptInvalid), "%s: %v", name, err)
	}
}

func TestReceiptFingerprintBindsTheRequest(t *testing.T) {
	in := ReceiptInput{MaterialID: uuid.New(), Quantity: 2, Unit: "kg", InvoiceStatus: repositories.ReceiptNoInvoice,
		IdempotencyKey: "synthetic-key"}
	same := in
	same.Unit = " KG "                  // normalized
	same.IdempotencyKey = "another key" // the key is not part of the request
	assert.Equal(t, in.Fingerprint(), same.Fingerprint())
	for name, mutate := range map[string]func(*ReceiptInput){
		"quantity":    func(r *ReceiptInput) { r.Quantity = 3 },
		"received_at": func(r *ReceiptInput) { r.ReceivedAt = time.Now() },
		"lot":         func(r *ReceiptInput) { r.LotNumber = "LOT-1" },
		"notes":       func(r *ReceiptInput) { r.Notes = "x" },
	} {
		other := in
		mutate(&other)
		assert.NotEqual(t, in.Fingerprint(), other.Fingerprint(), name)
	}
}
