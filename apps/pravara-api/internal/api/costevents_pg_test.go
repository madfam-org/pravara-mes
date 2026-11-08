package api

// Job consumption → Dhanam cost events on PostgreSQL, with the emitter
// connected as pravara_app (NOSUPERUSER NOBYPASSRLS): migration 034's outbox
// is tenant-scoped, survives a restart (a new emitter over the same table
// delivers what the previous one could not), corrections re-send the same
// id, and the table refuses money fields. Opt-in with
// PRAVARA_TEST_DATABASE_URL.

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/costevents/costeventstest"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/dispatchfakes"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

func TestCostEventsOutboxOnPostgres(t *testing.T) {
	admin := newScratchDatabase(t)
	app := openAsApp(t, admin)

	var tenantID, otherTenant, orderID uuid.UUID
	require.NoError(t, admin.QueryRow(`INSERT INTO tenants (id, name, slug) VALUES (gen_random_uuid(), 'MADFAM', 'madfam')
		ON CONFLICT (slug) DO UPDATE SET slug = EXCLUDED.slug RETURNING id`).Scan(&tenantID))
	require.NoError(t, admin.QueryRow(`INSERT INTO tenants (id, name, slug) VALUES (gen_random_uuid(), 'Otro taller', 'otro-taller')
		RETURNING id`).Scan(&otherTenant))
	require.NoError(t, admin.QueryRow(`INSERT INTO orders (tenant_id, customer_name, engagement_slug)
		VALUES ($1, 'Cliente sintético', 'taller-demo') RETURNING id`, tenantID).Scan(&orderID))

	completedAt := time.Now().UTC().Add(-2 * time.Hour).Truncate(time.Second)
	taskJob, dispatchJob := uuid.New(), uuid.New()
	insertFact := func(jobKind string, jobID uuid.UUID, order *uuid.UUID, qty, basis, source string, occurred time.Time) {
		t.Helper()
		_, err := admin.Exec(`INSERT INTO job_consumption (tenant_id, job_kind, job_id, order_id, kind, material_code,
			quantity, unit, basis, source, occurred_at) VALUES ($1, $2, $3, $4, 'material', 'FIL-PLA-BLK', $5, 'g', $6, $7, $8)`,
			tenantID, jobKind, jobID, order, qty, basis, source, occurred)
		require.NoError(t, err)
	}
	insertFact("task", taskJob, &orderID, "5.2", "estimate", "slicer", completedAt)
	insertFact("dispatch", dispatchJob, nil, "12.25", "actual", "operator", completedAt) // dispatch row gone: kept

	janua := dispatchfakes.NewJanua(t, dispatchfakes.Client{ID: "jnc_pravara_cost", Secret: "jns_test_only",
		Scope: costevents.WriteScope, Audience: costevents.Audience})
	dhanam := costeventstest.NewDhanam(t)
	log := logrus.New()
	log.SetLevel(logrus.PanicLevel)
	now := time.Now().UTC()
	newEmitter := func() *costevents.Emitter {
		tokens := machineclients.NewTokenSource(machineclients.Credentials{Name: "dhanam-cost-events",
			TokenURL: janua.TokenURL(), ClientID: "jnc_pravara_cost", ClientSecret: "jns_test_only",
			Scope: costevents.WriteScope, Audience: costevents.Audience}, nil)
		scope := func(ctx context.Context, fn func(ctx context.Context) error) error {
			return db.RunInTenantTx(ctx, app, tenantID.String(), fn)
		}
		e := costevents.NewEmitter(costevents.NewPGStore(db.NewTenantDB(app, log)),
			costevents.NewClient(dhanam.Endpoint(), tokens, nil), scope, costevents.Settings{Tenant: tenantID,
				Options:  costevents.Options{CostClass: "cogs", Start: completedAt.Add(-24 * time.Hour)},
				Lookback: 62 * 24 * time.Hour, BatchSize: 100}, log)
		e.SetClock(func() time.Time { return now })
		return e
	}
	run := func(e *costevents.Emitter) costevents.DeliverReport {
		t.Helper()
		_, err := e.Sweep(context.Background())
		require.NoError(t, err)
		rep, err := e.Deliver(context.Background())
		require.NoError(t, err)
		return rep
	}
	type row struct {
		status, lastErr, outcome string
		attempts                 int
	}
	read := func(id string) row {
		t.Helper()
		var r row
		require.NoError(t, admin.QueryRow(`SELECT status, COALESCE(last_error, ''), COALESCE(dhanam_outcome, ''), attempts
			FROM cost_event_outbox WHERE event_id = $1`, id).Scan(&r.status, &r.lastErr, &r.outcome, &r.attempts))
		return r
	}
	taskID := costevents.EventID("task", taskJob, "FIL-PLA-BLK")
	dispatchID := costevents.EventID("dispatch", dispatchJob, "FIL-PLA-BLK")

	// Dhanam is down: both rows stay pending with an attempt counted.
	dhanam.Script = []int{http.StatusServiceUnavailable}
	rep := run(newEmitter())
	assert.Equal(t, costevents.OutcomeTransient, rep.Stopped)
	assert.Equal(t, row{status: "pending", lastErr: "http_503", attempts: 1}, read(taskID))

	// Restart: a new emitter over the same table delivers them once due.
	now = now.Add(time.Minute)
	rep = run(newEmitter())
	assert.Equal(t, 2, rep.Delivered)
	assert.Equal(t, row{status: "delivered", outcome: "created"}, read(taskID))
	sent := dhanam.Stored[taskID]
	assert.Equal(t, map[string]any{"engagement": "eng:taller-demo"}, sent["attribution"])
	assert.Equal(t, "5.2", sent["quantity"])
	assert.Equal(t, "estimate", sent["basis"])
	assert.Equal(t, completedAt.Format(time.RFC3339), sent["occurred_at"])
	assert.NotContains(t, dhanam.Stored[dispatchID], "attribution", "no order: no engagement")
	assert.Equal(t, "12.25", dhanam.Stored[dispatchID]["quantity"])

	// Unchanged: nothing is sent again.
	requests := dhanam.Count()
	now = now.Add(5 * time.Minute)
	assert.Equal(t, 0, run(newEmitter()).Batches)
	assert.Equal(t, requests, dhanam.Count())

	// An operator's actual corrects the task line: the same id, updated.
	insertFact("task", taskJob, &orderID, "6", "actual", "operator", now)
	assert.Equal(t, 1, run(newEmitter()).Delivered)
	assert.Equal(t, row{status: "delivered", outcome: "updated"}, read(taskID))
	assert.Equal(t, "6", dhanam.Stored[taskID]["quantity"])

	// A body that changes while its POST is in flight is not claimed again
	// until that POST is settled: two bodies of one id never fly together.
	store := costevents.NewPGStore(db.NewTenantDB(app, log))
	inTenant := func(fn func(ctx context.Context) error) {
		t.Helper()
		require.NoError(t, db.RunInTenantTx(context.Background(), app, tenantID.String(), fn))
	}
	insertFact("task", taskJob, &orderID, "7", "actual", "printer", now.Add(time.Second))
	_, err := newEmitter().Sweep(context.Background())
	require.NoError(t, err)
	var first []costevents.Claimed
	inTenant(func(ctx context.Context) error {
		first, err = store.Claim(ctx, 10, now, costevents.Lease)
		return err
	})
	require.Len(t, first, 1)
	_, err = admin.Exec(`UPDATE orders SET engagement_slug = 'taller-demo-2' WHERE id = $1`, orderID)
	require.NoError(t, err)
	_, err = newEmitter().Sweep(context.Background())
	require.NoError(t, err)
	inTenant(func(ctx context.Context) error {
		again, err := store.Claim(ctx, 10, now, costevents.Lease)
		assert.Empty(t, again, "the in-flight lease holds the changed body back")
		return err
	})
	inTenant(func(ctx context.Context) error {
		return store.MarkDelivered(ctx, first[0].EventID, first[0].BodySHA256, "updated", now)
	})
	assert.Equal(t, "pending", read(taskID).status, "the old body's answer does not settle the new body")
	inTenant(func(ctx context.Context) error {
		next, err := store.Claim(ctx, 10, now, costevents.Lease)
		require.Len(t, next, 1)
		assert.NotEqual(t, first[0].BodySHA256, next[0].BodySHA256)
		return err
	})

	// Tenant isolation: another tenant's scope sees none of these rows.
	var visible int
	require.NoError(t, db.RunInTenantTx(context.Background(), app, otherTenant.String(), func(ctx context.Context) error {
		counts, err := costevents.NewPGStore(db.NewTenantDB(app, log)).Counts(ctx)
		for _, n := range counts {
			visible += n
		}
		return err
	}))
	assert.Zero(t, visible)

	// The table itself refuses money in a body (C12).
	_, err = admin.Exec(`INSERT INTO cost_event_outbox (tenant_id, event_id, job_kind, job_id, material_code, body, body_sha256)
		VALUES ($1, 'pravara.consumption.task.x', 'task', $2, 'FIL', '{"amount_minor": 100}', repeat('0', 64))`, tenantID, uuid.New())
	require.Error(t, err)
}
