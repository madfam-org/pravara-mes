package costevents

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	// The start date is a civil date in America/Mexico_City (Dhanam's
	// period cut-off); embed the zone so it never depends on the image.
	_ "time/tzdata"

	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/config"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/observability"
)

// PeriodZone is where Dhanam cuts periods (and where the start date is a
// civil date).
const PeriodZone = "America/Mexico_City"

// FromConfig builds the emitter from configuration. It returns (nil, nil)
// when the switch is off: nothing is built and nothing is required. When the
// switch is on but the configuration is incomplete it returns the problems
// (never a value: no secret can appear) and no emitter.
func FromConfig(cfg *config.Config, pool *sql.DB, log *logrus.Logger) (*Emitter, []string) {
	ce := cfg.CostEvents
	if !ce.Enabled {
		return nil, nil
	}
	if problems := ce.Problems(cfg.MachineClients.TokenURL); len(problems) > 0 {
		return nil, problems
	}
	loc, err := time.LoadLocation(PeriodZone)
	if err != nil {
		return nil, []string{fmt.Sprintf("time zone %s unavailable", PeriodZone)}
	}
	start, _ := ce.Start(loc)
	tenantID, _ := ce.Tenant()

	var poster Poster
	if !ce.DryRun {
		tokens := machineclients.NewTokenSource(machineclients.Credentials{
			Name: "dhanam-cost-events", TokenURL: ce.EffectiveTokenURL(cfg.MachineClients.TokenURL),
			ClientID: ce.ClientID, ClientSecret: ce.ClientSecret,
			Scope: TokenScope, Audience: Audience,
		}, nil)
		poster = NewClient(ce.Endpoint(), tokens, nil)
	}
	scope := func(ctx context.Context, fn func(ctx context.Context) error) error {
		return db.RunInTenantTx(ctx, pool, tenantID.String(), fn)
	}
	store := NewPGStore(db.NewTenantDB(pool, log))
	return NewEmitter(store, poster, scope, Settings{
		Tenant:    tenantID,
		Options:   Options{CostClass: ce.CostClass, Start: start},
		Lookback:  time.Duration(ce.LookbackDays) * 24 * time.Hour,
		BatchSize: ce.BatchSize,
		Interval:  time.Duration(ce.IntervalSeconds) * time.Second,
		DryRun:    ce.DryRun,
	}, log), nil
}

// StartFromConfig starts the emitter when the switch is on and the
// configuration is complete. A switched-on but incomplete configuration is
// logged as an error and leaves the emitter (and only it) stopped: the MES
// keeps running, the gauge pravara_api_cost_events_ready stays 0.
func StartFromConfig(ctx context.Context, cfg *config.Config, pool *sql.DB, log *logrus.Logger) *Emitter {
	observability.CostEventsReady.Set(0)
	e, problems := FromConfig(cfg, pool, log)
	if len(problems) > 0 {
		log.WithField("problems", problems).Error("cost events: PRAVARA_COST_EVENTS_ENABLED is on but the emitter did not start")
		return nil
	}
	if e == nil {
		return nil
	}
	observability.CostEventsReady.Set(1)
	log.WithFields(logrus.Fields{"tenant_id": e.set.Tenant, "dry_run": e.set.DryRun,
		"start": e.set.Options.Start.Format("2006-01-02")}).Info("cost events: emitter started")
	e.Start(ctx)
	return e
}
