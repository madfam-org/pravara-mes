// Package fabprep is pravara's client for the fabrication-prep slicing
// service (MES-1 §4): POST /v1/slice-jobs, GET /v1/slice-jobs/{id},
// GET /v1/profiles, and the signed slicer-variables document. Slicing,
// profiles and overrides validation all live in fabrication-prep; pravara
// only routes JSON and digests.
package fabprep

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/integrations/remote"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/machineclients"
)

const service = "fabrication-prep"

// Slice targets.
const (
	TargetKlipperGcode = "klipper_gcode"
	TargetBambu3MF     = "bambu_3mf"
)

// Job statuses.
const (
	StatusQueued       = "queued"
	StatusRunning      = "running"
	StatusSucceeded    = "succeeded"
	StatusFailed       = "failed"
	StatusDeadLettered = "dead_lettered"
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,200}$`)

// Client calls fabrication-prep.
type Client struct {
	baseURL *url.URL
	http    *http.Client
	tokens  *machineclients.TokenSource
}

// NewClient creates a client for baseURL (e.g. https://fabrication-prep-api.madfam.io).
func NewClient(baseURL string, tokens *machineclients.TokenSource, httpClient *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("fabrication-prep: invalid base URL %q", baseURL)
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &Client{baseURL: u, http: httpClient, tokens: tokens}, nil
}

// Ref is a downloadable input reference.
type Ref struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

// Input is the geometry to slice (model/stl or model/3mf) with its GOC-1 sidecar.
type Input struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	Variables *Ref   `json:"variables,omitempty"`
}

// SliceJobRequest is POST /v1/slice-jobs (MES-1 §4). Requirements is the
// product RequirementProfile; fabrication-prep rejects overrides outside it.
type SliceJobRequest struct {
	Input           Input          `json:"input"`
	PrinterProfile  string         `json:"printer_profile"`
	FilamentProfile string         `json:"filament_profile"`
	ProcessProfile  string         `json:"process_profile"`
	Overrides       map[string]any `json:"overrides,omitempty"`
	Requirements    any            `json:"requirements,omitempty"`
	Part            string         `json:"part,omitempty"`
	Target          string         `json:"target"`
}

// ProfileRef is a resolved profile with its content digest.
type ProfileRef struct {
	ID      string `json:"id"`
	Version int    `json:"version"`
	SHA256  string `json:"sha256"`
}

// SignedArtifact is a short-lived signed download.
type SignedArtifact struct {
	URL       string `json:"url"`
	SHA256    string `json:"sha256"`
	MediaType string `json:"media_type"`
	Bytes     int64  `json:"bytes"`
	Filename  string `json:"filename,omitempty"`
	ExpiresAt string `json:"expires_at"`
}

// JobError is a failed job's reason.
type JobError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// SliceJob is the job view.
type SliceJob struct {
	ID              string                `json:"id"`
	Status          string                `json:"status"`
	Target          string                `json:"target"`
	Attempts        int                   `json:"attempts"`
	MaxAttempts     int                   `json:"max_attempts"`
	Profiles        map[string]ProfileRef `json:"profiles"`
	Error           *JobError             `json:"error"`
	Output          *SignedArtifact       `json:"output"`
	SlicerVariables *SignedArtifact       `json:"slicer_variables"`
	Estimates       map[string]any        `json:"estimates,omitempty"`
}

// Terminal reports whether the job will not change any more.
func (j *SliceJob) Terminal() bool {
	switch j.Status {
	case StatusSucceeded, StatusFailed, StatusDeadLettered:
		return true
	}
	return false
}

// CreateSliceJob submits a job. idempotencyKey makes resubmission safe: the
// same key and body return the existing job (200).
func (c *Client) CreateSliceJob(ctx context.Context, req SliceJobRequest, idempotencyKey string) (*SliceJob, error) {
	if !idempotencyKeyPattern.MatchString(idempotencyKey) {
		return nil, fmt.Errorf("fabrication-prep: invalid idempotency key")
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("fabrication-prep: encode slice job: %w", err)
	}
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.resolve("/v1/slice-jobs"), bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", idempotencyKey)
		return r, nil
	})
	if err != nil {
		return nil, remote.Wrap(service, "create slice job", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusAccepted && resp.StatusCode != http.StatusOK {
		// 422: overrides or requirements rejected (terminal, with the path);
		// 409: the key was reused with another body (terminal).
		return nil, remote.StatusError(service, "create slice job", resp)
	}
	return decodeJob(resp, "create slice job")
}

// GetSliceJob reads a job; succeeded jobs carry fresh signed URLs.
func (c *Client) GetSliceJob(ctx context.Context, id string) (*SliceJob, error) {
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("/v1/slice-jobs/"+url.PathEscape(id)), nil)
	})
	if err != nil {
		return nil, remote.Wrap(service, "read slice job", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, remote.StatusError(service, "read slice job", resp)
	}
	return decodeJob(resp, "read slice job")
}

func decodeJob(resp *http.Response, op string) (*SliceJob, error) {
	var job SliceJob
	if _, err := remote.ReadJSON(resp, &job); err != nil || job.ID == "" {
		msg := "job view has no id"
		if err != nil {
			msg = err.Error()
		}
		return nil, &remote.Error{Service: service, Op: op, Status: resp.StatusCode, Message: msg, Retryable: true}
	}
	return &job, nil
}

// Profile is one catalog entry of GET /v1/profiles.
type Profile struct {
	ID                 string   `json:"id"`
	Version            int      `json:"version"`
	Ref                string   `json:"ref"`
	Kind               string   `json:"kind"`
	SHA256             string   `json:"sha256"`
	Target             string   `json:"target,omitempty"`
	MaterialClass      string   `json:"material_class,omitempty"`
	RequiresProcessTag string   `json:"requires_process_tag,omitempty"`
	Printers           []string `json:"printers,omitempty"`
	Tags               []string `json:"tags,omitempty"`
}

// Catalog is GET /v1/profiles.
type Catalog struct {
	Profiles []Profile `json:"profiles"`
}

// ListProfiles reads the profile catalog.
func (c *Client) ListProfiles(ctx context.Context) (*Catalog, error) {
	resp, err := machineclients.Do(ctx, c.http, c.tokens, func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, c.resolve("/v1/profiles"), nil)
	})
	if err != nil {
		return nil, remote.Wrap(service, "list profiles", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, remote.StatusError(service, "list profiles", resp)
	}
	var cat Catalog
	if _, err := remote.ReadJSON(resp, &cat); err != nil {
		return nil, &remote.Error{Service: service, Op: "list profiles", Message: err.Error(), Retryable: true}
	}
	return &cat, nil
}

// SlicerVariables is the subset of slicer-variables.json pravara records in
// the ManufacturingRecord.
type SlicerVariables struct {
	Format          string                `json:"format"`
	FormatVersion   string                `json:"format_version"`
	JobID           string                `json:"job_id"`
	Target          string                `json:"target"`
	Profiles        map[string]ProfileRef `json:"profiles"`
	MaterialClass   string                `json:"material_class"`
	EffectiveSHA256 string                `json:"effective_sha256"`
	RequirementsSHA string                `json:"requirements_sha256"`
	GeneratorOutput *struct {
		InstanceID      string `json:"instance_id"`
		VariablesSHA256 string `json:"variables_sha256"`
	} `json:"generator_output,omitempty"`
	Slicer struct {
		Name    string `json:"name"`
		Version string `json:"version"`
	} `json:"slicer"`
	Output struct {
		SHA256      string `json:"sha256"`
		MediaType   string `json:"media_type"`
		Bytes       int64  `json:"bytes"`
		Filename    string `json:"filename"`
		GcodeSHA256 string `json:"gcode_sha256,omitempty"`
	} `json:"output"`
	Warnings []any `json:"warnings"`
}

// SlicerVariablesFormat identifies the document.
const SlicerVariablesFormat = "madfam.fabrication-prep.slicer-variables"

// FetchSlicerVariables downloads the signed document (no token: the URL is
// the capability) and verifies its bytes against the job's digest.
func (c *Client) FetchSlicerVariables(ctx context.Context, art *SignedArtifact) (*SlicerVariables, error) {
	if art == nil || art.URL == "" {
		return nil, &remote.Error{Service: service, Op: "slicer variables", Message: "job has no slicer_variables"}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, art.URL, nil)
	if err != nil {
		return nil, &remote.Error{Service: service, Op: "slicer variables", Message: "invalid signed URL"}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// The signed URL is a capability: never echo it.
		return nil, &remote.Error{Service: service, Op: "slicer variables", Message: "download failed", Retryable: true}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		e := remote.StatusError(service, "slicer variables", resp)
		e.Retryable = e.Retryable || resp.StatusCode == http.StatusForbidden // expired signature: re-read the job
		return nil, e
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, remote.MaxBody))
	if err != nil {
		return nil, &remote.Error{Service: service, Op: "slicer variables", Message: "read failed", Retryable: true}
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != art.SHA256 {
		return nil, &remote.Error{Service: service, Op: "slicer variables", Code: "digest_mismatch",
			Message: "slicer-variables bytes do not match the job's sha256"}
	}
	var doc SlicerVariables
	if err := json.Unmarshal(raw, &doc); err != nil || doc.Format != SlicerVariablesFormat {
		return nil, &remote.Error{Service: service, Op: "slicer variables", Code: "invalid_document",
			Message: "not a slicer-variables document"}
	}
	return &doc, nil
}

func (c *Client) resolve(path string) string {
	return c.baseURL.String() + path
}
