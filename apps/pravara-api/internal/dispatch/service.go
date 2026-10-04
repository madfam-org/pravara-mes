// Package dispatch runs fabrication dispatch (MES-1 §5-§7):
//
//	task → order item → product (cartridge, mode, parameters, type shell)
//	  → RequirementProfile from the type shell (asset-shells)
//	  → matchmaking v2 + a TTL reservation on the best idle machine
//	  → yantra4d render bundle (geometry + GOC-1 variables.json)
//	  → fabrication-prep slice job with the machine's profiles (polled)
//	  → re-check the machine, then start_job (signed URL + sha256) on the
//	    durable command stream, ledger row committed first
//	  → on Job/Status = complete for that command and machine: genealogy,
//	    ManufacturingRecord, and the instance shell + passport through an
//	    outbox to asset-shells.
//
// Every hop records its state in dispatch_jobs, is idempotent on replay
// (Idempotency-Key on the slice job, a fixed command id per enqueue), and
// fails as retryable (backed off, bounded) or terminal (with a reason).
// pravara does no geometry: it routes JSON, digests and state.
package dispatch

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/fabrication"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/assetshells"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/fabprep"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/yantra4d"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/matchmaking"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/pubsub"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// CompletionEventTypes are the outbox event types that signal a completed
// job. task.job_completed is written in the same transaction that marks the
// start_job ledger row completed (telemetry-worker ack path, #51); the
// Sparkplug host reports Job/Status = complete through that path.
var CompletionEventTypes = []string{"task.job_completed"}

// Renderer is the yantra4d surface dispatch uses.
type Renderer interface {
	Render(ctx context.Context, req yantra4d.RenderRequest) (*yantra4d.RenderResponse, error)
	FetchSidecar(ctx context.Context, part yantra4d.Part) (*yantra4d.Sidecar, string, error)
	AbsoluteURL(ref string) string
}

// Slicer is the fabrication-prep surface dispatch uses.
type Slicer interface {
	CreateSliceJob(ctx context.Context, req fabprep.SliceJobRequest, idempotencyKey string) (*fabprep.SliceJob, error)
	GetSliceJob(ctx context.Context, id string) (*fabprep.SliceJob, error)
	ListProfiles(ctx context.Context) (*fabprep.Catalog, error)
	FetchSlicerVariables(ctx context.Context, art *fabprep.SignedArtifact) (*fabprep.SlicerVariables, error)
}

// Shells is the asset-shells surface dispatch uses.
type Shells interface {
	GetShell(ctx context.Context, shellID string) (*assetshells.Shell, error)
	FindTypeShells(ctx context.Context, pairs map[string]string) ([]assetshells.Shell, error)
	GetSubmodel(ctx context.Context, shellID, submodelID string) (map[string]any, error)
	PublishInstance(ctx context.Context, ts *machineclients.TokenSource, env []byte) (*assetshells.PublishResult, error)
	AppendPassportEvent(ctx context.Context, ts *machineclients.TokenSource, instanceUUID string, body []byte) (*assetshells.PublishResult, error)
}

// CommandEnqueuer appends a command to the durable stream (pubsub.Publisher).
type CommandEnqueuer interface {
	PublishCommandForDispatch(ctx context.Context, tenantID uuid.UUID, data pubsub.MachineCommandData) error
}

// GenealogyCreator creates the draft genealogy record of a completed task.
type GenealogyCreator interface {
	AutoCreateFromTask(ctx context.Context, task services.TaskInfo) (*repositories.ProductGenealogy, error)
}

// Settings tune the service.
type Settings struct {
	Enabled            bool
	RenderFormat       string
	ReservationTTL     time.Duration
	CommandHold        time.Duration
	MaxAttempts        int
	PollInterval       time.Duration
	PassportAttempts   int
	RequireBoundingBox bool
	// MatchWait bounds how long a dispatch waits for an eligible machine.
	MatchWait time.Duration
}

func (s *Settings) defaults() {
	if s.MatchWait <= 0 {
		s.MatchWait = 24 * time.Hour
	}
	if s.RenderFormat == "" {
		s.RenderFormat = "3mf"
	}
	if s.ReservationTTL <= 0 {
		s.ReservationTTL = 15 * time.Minute
	}
	if s.CommandHold <= 0 {
		s.CommandHold = 6 * time.Hour
	}
	if s.MaxAttempts <= 0 {
		s.MaxAttempts = 5
	}
	if s.PollInterval <= 0 {
		s.PollInterval = 10 * time.Second
	}
	if s.PassportAttempts <= 0 {
		s.PassportAttempts = 20
	}
}

// Deps are the collaborators of the service.
type Deps struct {
	Pool      *sql.DB // for per-item tenant transactions
	Dispatch  *repositories.DispatchRepository
	Sources   *repositories.DispatchSources
	Passports *repositories.PassportRepository
	Ledger    *repositories.TaskCommandRepository
	Live      matchmaking.LiveStateReader
	Renderer  Renderer
	Slicer    Slicer
	Shells    Shells
	Enqueuer  CommandEnqueuer
	Genealogy GenealogyCreator
	Clients   *machineclients.Set
}

// Service is the dispatcher, matchmaker and passport updater.
type Service struct {
	Deps
	cfg   Settings
	log   *logrus.Logger
	now   func() time.Time
	owner string
}

// NewService creates the service.
func NewService(deps Deps, cfg Settings, log *logrus.Logger) *Service {
	cfg.defaults()
	return &Service{Deps: deps, cfg: cfg, log: log, now: func() time.Time { return time.Now().UTC() },
		owner: "pravara-api/" + uuid.NewString()[:8]}
}

// Enabled reports whether dispatch (not the dry run) is turned on.
func (s *Service) Enabled() bool { return s.cfg.Enabled }

// Unavailable explains why dispatch cannot run ("" when it can).
func (s *Service) Unavailable() string {
	switch {
	case !s.cfg.Enabled:
		return "fabrication dispatch is disabled (DISPATCH_ENABLED=false)"
	case s.Enqueuer == nil:
		return "the durable command stream is not configured (REDIS_URL)"
	case s.Clients == nil || !s.Clients.Yantra4D.Configured():
		return "the yantra4d machine client has no credentials (pravara-service-clients)"
	case !s.Clients.FabricationPrep.Configured():
		return "the fabrication-prep machine client has no credentials (pravara-service-clients)"
	case s.Renderer == nil || s.Slicer == nil || s.Shells == nil:
		return "dispatch service clients are not configured"
	}
	return ""
}

// stepError is a classified failure of one hop.
type stepError struct {
	code      string
	msg       string
	retryable bool
	// wait marks a condition expected to clear on its own (no idle machine
	// qualifies yet). It is retried without spending the attempt budget,
	// until the dispatch has waited longer than Settings.MatchWait.
	wait bool
}

func (e *stepError) Error() string { return e.code + ": " + e.msg }

func terminal(code, format string, args ...any) *stepError {
	return &stepError{code: code, msg: fmt.Sprintf(format, args...)}
}

func retryable(code, format string, args ...any) *stepError {
	return &stepError{code: code, msg: fmt.Sprintf(format, args...), retryable: true}
}

// MatchOutcome is the answer of a dry run.
type MatchOutcome struct {
	TaskID        *uuid.UUID                      `json:"task_id,omitempty"`
	OrderItemID   *uuid.UUID                      `json:"order_item_id,omitempty"`
	Product       ProductSpec                     `json:"product"`
	Requirements  *fabrication.RequirementProfile `json:"requirements"`
	Effective     fabrication.RequirementSet      `json:"effective_requirements"`
	Result        matchmaking.Result              `json:"result"`
	DryRun        bool                            `json:"dry_run"`
	EvaluatedAt   time.Time                       `json:"evaluated_at"`
	SlicingSource string                          `json:"slicing_catalog"`
}

// Match evaluates every machine for a task without reserving anything. ctx
// carries the request's tenant scope.
func (s *Service) Match(ctx context.Context, tenantID, taskID uuid.UUID) (*MatchOutcome, error) {
	tc, err := s.Sources.TaskContext(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if tc == nil {
		return nil, terminal("task_not_found", "task %s not found", taskID)
	}
	spec, serr := s.resolveProduct(ctx, tc)
	if serr != nil {
		return nil, serr
	}
	profile, serr := s.requirementProfile(ctx, spec.TypeShellID)
	if serr != nil {
		return nil, serr
	}
	catalog, source := s.catalog(ctx)
	res, err := s.rank(ctx, tenantID, profile, spec, catalog, nil)
	if err != nil {
		return nil, err
	}
	return &MatchOutcome{TaskID: &taskID, OrderItemID: tc.OrderItemID, Product: *spec, Requirements: profile,
		Effective: profile.ForPart(spec.Part), Result: *res, DryRun: true, EvaluatedAt: s.now(), SlicingSource: source}, nil
}

// catalog reads fabrication-prep's profile catalog; nil when the client is
// not configured or the call fails (reported, not fatal, in dry runs).
func (s *Service) catalog(ctx context.Context) ([]matchmaking.SlicingProfile, string) {
	if s.Slicer == nil || s.Clients == nil || !s.Clients.FabricationPrep.Configured() {
		return nil, "not consulted (fabrication-prep client not configured)"
	}
	cat, err := s.Slicer.ListProfiles(ctx)
	if err != nil {
		return nil, "unavailable: " + err.Error()
	}
	out := make([]matchmaking.SlicingProfile, 0, len(cat.Profiles))
	for _, p := range cat.Profiles {
		out = append(out, matchmaking.SlicingProfile{Ref: p.Ref, ID: p.ID, Kind: p.Kind, Target: p.Target,
			MaterialClass: p.MaterialClass, RequiresProcessTag: p.RequiresProcessTag, Printers: p.Printers, Tags: p.Tags})
	}
	return out, "fabrication-prep"
}

// rank runs matchmaking over the tenant's machines. self is the dispatch
// doing the match: its own reservation does not count against a machine.
func (s *Service) rank(ctx context.Context, tenantID uuid.UUID, profile *fabrication.RequirementProfile, spec *ProductSpec,
	catalog []matchmaking.SlicingProfile, self *uuid.UUID) (*matchmaking.Result, error) {
	machines, err := s.Sources.MatchMachines(ctx)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(machines))
	for i := range machines {
		ids = append(ids, machines[i].ID)
		if self != nil && machines[i].ReservedBy != nil && *machines[i].ReservedBy == *self {
			machines[i].ReservedBy = nil
		}
	}
	live := map[uuid.UUID]matchmaking.LiveState{}
	var gaps []string
	if s.Live != nil {
		live, err = s.Live.LiveStates(ctx, tenantID, ids)
		if err != nil {
			s.log.WithError(err).Warn("dispatch: live machine state unavailable")
			live, gaps = map[uuid.UUID]matchmaking.LiveState{}, []string{matchmaking.GapLiveState}
		}
	} else {
		gaps = []string{matchmaking.GapLiveState}
	}
	res := matchmaking.Rank(matchmaking.Inputs{
		Requirements:       profile.ForPart(spec.Part),
		BoundingBox:        spec.BoundingBox,
		RequireBoundingBox: s.cfg.RequireBoundingBox,
		Catalog:            catalog,
	}, machines, live)
	res.Gaps = append(res.Gaps, gaps...)
	return &res, nil
}

// Request files a dispatch for a task. ctx carries the request's tenant scope.
func (s *Service) Request(ctx context.Context, tenantID, taskID uuid.UUID, requestedBy, actor *uuid.UUID) (*repositories.DispatchJob, error) {
	tc, err := s.Sources.TaskContext(ctx, taskID)
	if err != nil {
		return nil, err
	}
	if tc == nil {
		return nil, terminal("task_not_found", "task %s not found", taskID)
	}
	if tc.OrderItemID == nil {
		return nil, terminal("task_without_order_item", "task %s has no order item to fabricate", taskID)
	}
	if _, serr := s.resolveProduct(ctx, tc); serr != nil {
		return nil, serr
	}
	d := &repositories.DispatchJob{TenantID: tenantID, TaskID: taskID, OrderItemID: tc.OrderItemID,
		ProductDefinitionID: tc.ProductID, MaxAttempts: s.cfg.MaxAttempts, RequestedBy: requestedBy, RequestedByActor: actor}
	if err := s.Dispatch.Create(ctx, d); err != nil {
		return nil, err
	}
	return d, nil
}

// ErrorInfo exposes a classified dispatch error to HTTP handlers.
func ErrorInfo(err error) (code, msg string, isRetryable, ok bool) {
	var se *stepError
	if errors.As(err, &se) {
		return se.code, se.msg, se.retryable, true
	}
	return "", "", false, false
}
