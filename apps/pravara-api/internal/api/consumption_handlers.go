package api

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
	"github.com/madfam-org/pravara-mes/packages/sdk-go/pkg/types"
)

// ConsumptionHandler serves job consumption: the read for downstream cost
// ledgers (quantities only, no money) and operator reports of actuals.
//
// Scope decision (route_scopes.go): GET /v1/jobs/consumption accepts
// pravara-mes:read or pravara-mes:jobs. POST /v1/tasks/:id/consumption moves
// stock, so it is for people (and wildcard API keys) only.
type ConsumptionHandler struct {
	repo *repositories.JobConsumptionRepository
	svc  *services.ConsumptionService
	log  *logrus.Logger
}

// NewConsumptionHandler creates the handler.
func NewConsumptionHandler(repo *repositories.JobConsumptionRepository, svc *services.ConsumptionService, log *logrus.Logger) *ConsumptionHandler {
	return &ConsumptionHandler{repo: repo, svc: svc, log: log}
}

const (
	consumptionDefaultLimit = 100
	consumptionMaxLimit     = 1000
	materialFieldMaxLen     = 100
)

func badRequest(c *gin.Context, code, msg string) {
	c.JSON(http.StatusBadRequest, gin.H{"error": code, "message": msg})
}

// List is GET /v1/jobs/consumption.
// @Summary List job consumption
// @Description Per-job consumption rows, quantities only (no cost): material (g, mm, ml, sheet) and machine time (min), each with basis (actual or estimate) and source (printer, operator or slicer). engagement_slug is the order's current engagement. registry_code is the material registry code material_code resolves to (omitted when unregistered). Rows are ordered by occurred_at (the job's completion); from is inclusive and to exclusive.
// @Tags jobs
// @Produce json
// @Param from query string false "occurred_at >= from (RFC3339)"
// @Param to query string false "occurred_at < to (RFC3339)"
// @Param task_id query string false "Filter by task (UUID)"
// @Param order_id query string false "Filter by order (UUID)"
// @Param engagement_slug query string false "Filter by the order's engagement slug"
// @Param limit query int false "Page size (max 1000)" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} ListResponse{data=[]repositories.JobConsumption} "Paginated consumption rows"
// @Failure 400 {object} map[string]string "Invalid filter"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /jobs/consumption [get]
func (h *ConsumptionHandler) List(c *gin.Context) {
	f, ferr := parseJobConsumptionFilter(c.GetQuery)
	if ferr != nil {
		badRequest(c, ferr.Code, ferr.Message)
		return
	}
	rows, total, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		h.log.WithError(err).Error("Failed to list job consumption")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to list job consumption"})
		return
	}
	c.JSON(http.StatusOK, ListResponse{Data: rows, Total: total, Limit: f.Limit, Offset: f.Offset})
}

// coverageMaxCodes caps the unresolved codes listed by Coverage.
const coverageMaxCodes = 200

// Coverage is GET /v1/jobs/consumption/coverage.
// @Summary How many consumption rows carry a registry code
// @Description The RFC 0058 wave 1 gate is that every material consumption row carries a material registry code. A row's code resolves at read time: the code itself, an alias, or a linked stock item's SKU. Counts material rows (occurred_at in [from, to) when given) with and without a registry code, and lists the unresolved codes, most rows first (at most 200). Register, alias or link each one; nothing is guessed.
// @Tags jobs
// @Produce json
// @Param from query string false "occurred_at >= from (RFC3339)"
// @Param to query string false "occurred_at < to (RFC3339)"
// @Success 200 {object} repositories.ConsumptionCoverage
// @Failure 400 {object} map[string]string "invalid_from or invalid_to"
// @Security BearerAuth
// @Router /jobs/consumption/coverage [get]
func (h *ConsumptionHandler) Coverage(c *gin.Context) {
	var from, to *time.Time
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &from}, {"to", &to}} {
		if raw := c.Query(p.name); raw != "" {
			t, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				badRequest(c, "invalid_"+p.name, p.name+" must be an RFC3339 timestamp")
				return
			}
			*p.dst = &t
		}
	}
	cov, err := h.repo.Coverage(c.Request.Context(), from, to, coverageMaxCodes)
	if err != nil {
		h.log.WithError(err).Error("Failed to compute consumption coverage")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to compute coverage"})
		return
	}
	c.JSON(http.StatusOK, cov)
}

// jobConsumptionParams are the query parameters of GET /v1/jobs/consumption,
// in the order the OpenAPI document lists them. The MCP tool
// mes_job_consumption takes exactly these arguments (mcp_tools.go;
// TestMCPJobConsumptionSchemaMatchesOpenAPI).
var jobConsumptionParams = []string{"from", "to", "task_id", "order_id", "engagement_slug", "limit", "offset"}

// filterError is an invalid job consumption filter: Code is the HTTP error
// code ("invalid_from", ...).
type filterError struct {
	Code    string
	Message string
}

// parseJobConsumptionFilter reads the job consumption filter through get
// (the HTTP query, or the MCP tool's arguments), so both surfaces validate
// identically. An empty value counts as absent, except engagement_slug,
// which is validated whenever it is present.
func parseJobConsumptionFilter(get func(name string) (string, bool)) (repositories.JobConsumptionFilter, *filterError) {
	f := repositories.JobConsumptionFilter{Limit: consumptionDefaultLimit}
	value := func(name string) string {
		raw, _ := get(name)
		return raw
	}
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if raw := value(p.name); raw != "" {
			t, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				return f, &filterError{"invalid_" + p.name, p.name + " must be an RFC3339 timestamp"}
			}
			*p.dst = &t
		}
	}
	for _, p := range []struct {
		name string
		dst  **uuid.UUID
	}{{"task_id", &f.TaskID}, {"order_id", &f.OrderID}} {
		if raw := value(p.name); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				return f, &filterError{"invalid_" + p.name, p.name + " is not a UUID"}
			}
			*p.dst = &id
		}
	}
	if raw, ok := get("engagement_slug"); ok {
		if !types.ValidEngagementSlug(raw) {
			return f, &filterError{"invalid_engagement_slug", "engagement_slug must match " + types.EngagementSlugPattern}
		}
		f.EngagementSlug = &raw
	}
	if raw := value("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > consumptionMaxLimit {
			return f, &filterError{"invalid_limit", "limit must be 1-1000"}
		}
		f.Limit = n
	}
	if raw := value("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return f, &filterError{"invalid_offset", "offset must be >= 0"}
		}
		f.Offset = n
	}
	return f, nil
}

// ConsumptionReportRequest is an operator-reported actual for a task.
type ConsumptionReportRequest struct {
	// MaterialCode is required for material (unit g, mm, ml or sheet): the
	// inventory SKU when the material is stocked. Omit it for machine time
	// (unit min).
	MaterialCode string   `json:"material_code"`
	MaterialLot  string   `json:"material_lot"`
	Quantity     *float64 `json:"quantity" binding:"required"`
	Unit         string   `json:"unit" binding:"required"`
}

// Report is POST /v1/tasks/:id/consumption.
// @Summary Report a task's actual consumption
// @Description Records an operator-reported actual (basis actual, source operator) for a task: material with material_code and unit g, mm, ml or sheet, or machine time with unit min. It belongs to the task's latest completed dispatch when there is one. When material_code is the SKU of an inventory item in the same unit, stock relieved for the job is brought to this actual (an adjustment against the slicer estimate already relieved). The same actual can be reported once per job and material (409 already_recorded).
// @Tags tasks
// @Accept json
// @Produce json
// @Param id path string true "Task ID (UUID)"
// @Param body body ConsumptionReportRequest true "Actual consumption"
// @Success 201 {object} map[string]interface{} "{consumption, stock_movements}"
// @Failure 400 {object} map[string]string "Invalid report"
// @Failure 404 {object} map[string]string "Task not found"
// @Failure 409 {object} map[string]interface{} "Already recorded {error, consumption}"
// @Security BearerAuth
// @Router /tasks/{id}/consumption [post]
func (h *ConsumptionHandler) Report(c *gin.Context) {
	taskID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "invalid_id", "Invalid task ID format")
		return
	}
	rawTenant, _ := middleware.GetTenantID(c)
	tenantID, err := uuid.Parse(rawTenant)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "message": "no tenant context"})
		return
	}
	var req ConsumptionReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	if len(req.MaterialCode) > materialFieldMaxLen || len(req.MaterialLot) > materialFieldMaxLen {
		badRequest(c, "validation_error", "material_code and material_lot are at most 100 characters")
		return
	}
	report := services.OperatorReport{MaterialCode: req.MaterialCode, MaterialLot: req.MaterialLot,
		Quantity: *req.Quantity, Unit: req.Unit}
	if actor := middleware.ActorUUID(c); actor != uuid.Nil {
		report.ReportedBy = &actor
	}
	fact, created, moves, err := h.svc.RecordOperatorReport(c.Request.Context(), tenantID, taskID, report)
	switch {
	case errors.Is(err, services.ErrConsumptionTaskNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "task_not_found", "message": "Task not found"})
		return
	case errors.Is(err, services.ErrConsumptionInvalid):
		badRequest(c, "validation_error", err.Error())
		return
	case err != nil:
		h.log.WithError(err).WithField("task_id", taskID).Error("Failed to record operator consumption")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to record consumption"})
		return
	}
	if !created {
		c.JSON(http.StatusConflict, gin.H{"error": "already_recorded",
			"message": "an operator actual for this job and material is already recorded", "consumption": fact})
		return
	}
	if moves == nil {
		moves = []repositories.InventoryTransaction{}
	}
	c.JSON(http.StatusCreated, gin.H{"consumption": fact, "stock_movements": moves})
}
