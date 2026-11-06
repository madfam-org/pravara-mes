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
// @Description Per-job consumption rows, quantities only (no cost): material (g, mm, ml, sheet) and machine time (min), each with basis (actual or estimate) and source (printer, operator or slicer). engagement_slug is the order's current engagement. Rows are ordered by occurred_at (the job's completion); from is inclusive and to exclusive.
// @Tags jobs
// @Produce json
// @Param from query string false "occurred_at >= from (RFC3339)"
// @Param to query string false "occurred_at < to (RFC3339)"
// @Param task_id query string false "Filter by task (UUID)"
// @Param order_id query string false "Filter by order (UUID)"
// @Param engagement_slug query string false "Filter by the order's engagement slug"
// @Param limit query int false "Page size (max 1000)" default(100)
// @Param offset query int false "Offset" default(0)
// @Success 200 {object} ListResponse "data: []repositories.JobConsumption"
// @Failure 400 {object} map[string]string "Invalid filter"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /jobs/consumption [get]
func (h *ConsumptionHandler) List(c *gin.Context) {
	f := repositories.JobConsumptionFilter{Limit: consumptionDefaultLimit}
	for _, p := range []struct {
		name string
		dst  **time.Time
	}{{"from", &f.From}, {"to", &f.To}} {
		if raw := c.Query(p.name); raw != "" {
			t, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				badRequest(c, "invalid_"+p.name, p.name+" must be an RFC3339 timestamp")
				return
			}
			*p.dst = &t
		}
	}
	for _, p := range []struct {
		name string
		dst  **uuid.UUID
	}{{"task_id", &f.TaskID}, {"order_id", &f.OrderID}} {
		if raw := c.Query(p.name); raw != "" {
			id, err := uuid.Parse(raw)
			if err != nil {
				badRequest(c, "invalid_"+p.name, p.name+" is not a UUID")
				return
			}
			*p.dst = &id
		}
	}
	if raw, ok := c.GetQuery("engagement_slug"); ok {
		if !types.ValidEngagementSlug(raw) {
			badRequest(c, "invalid_engagement_slug", "engagement_slug must match "+types.EngagementSlugPattern)
			return
		}
		f.EngagementSlug = &raw
	}
	if raw := c.Query("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > consumptionMaxLimit {
			badRequest(c, "invalid_limit", "limit must be 1-1000")
			return
		}
		f.Limit = n
	}
	if raw := c.Query("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			badRequest(c, "invalid_offset", "offset must be >= 0")
			return
		}
		f.Offset = n
	}
	rows, total, err := h.repo.List(c.Request.Context(), f)
	if err != nil {
		h.log.WithError(err).Error("Failed to list job consumption")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to list job consumption"})
		return
	}
	c.JSON(http.StatusOK, ListResponse{Data: rows, Total: total, Limit: f.Limit, Offset: f.Offset})
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
