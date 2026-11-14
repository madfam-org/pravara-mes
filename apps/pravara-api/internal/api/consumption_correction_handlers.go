package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/services"
)

// Scope decision (route_scopes.go): corrections and print-job completion
// move stock, so, like operator reports, they are for people (and wildcard
// API keys) only. Neither sends anything to a machine.

// CorrectionRequest corrects a recorded consumption row.
type CorrectionRequest struct {
	Quantity *float64 `json:"quantity" binding:"required"`
	// Unit defaults to the corrected row's unit.
	Unit string `json:"unit"`
	// Disposition: good, scrap or failed_print (default: the corrected row's).
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
}

// Correct is POST /v1/jobs/consumption/:id/corrections.
// @Summary Correct a recorded consumption row
// @Description Append-only: stores a new operator actual of the same job, kind and material that supersedes the row (supersedes_id); history is never updated. The correction becomes the job's effective quantity and the stock relieved for the job moves by the difference (an adjustment). A row is corrected once: correct the latest correction to change it again (409 already_superseded names it). disposition (good, scrap, failed_print) is kept in Pravara and not sent to the cost ledger (its contract has no such field).
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Consumption row ID (UUID)"
// @Param body body CorrectionRequest true "Correction"
// @Success 201 {object} map[string]interface{} "{consumption, superseded, stock_movements}"
// @Failure 400 {object} map[string]string "invalid_id or validation_error"
// @Failure 404 {object} map[string]string "consumption_not_found"
// @Failure 409 {object} map[string]interface{} "already_superseded {error, message, superseded_by}"
// @Security BearerAuth
// @Router /jobs/consumption/{id}/corrections [post]
func (h *ConsumptionHandler) Correct(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "invalid_id", "id is not a UUID")
		return
	}
	var req CorrectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "validation_error", err.Error())
		return
	}
	in := services.Correction{Quantity: *req.Quantity, Unit: req.Unit, Disposition: req.Disposition, Reason: req.Reason}
	if a := middleware.ActorUUID(c); a != uuid.Nil {
		in.By = &a
	}
	res, err := h.svc.Correct(c.Request.Context(), id, in)
	switch {
	case errors.Is(err, services.ErrConsumptionNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "consumption_not_found", "message": "Consumption row not found"})
		return
	case errors.Is(err, services.ErrConsumptionSuperseded):
		body := gin.H{"error": "already_superseded", "message": err.Error()}
		if row, getErr := h.repo.Get(c.Request.Context(), id); getErr == nil && row != nil && row.SupersededBy != nil {
			body["superseded_by"] = row.SupersededBy
		}
		c.JSON(http.StatusConflict, body)
		return
	case errors.Is(err, services.ErrConsumptionInvalid):
		badRequest(c, "validation_error", err.Error())
		return
	case err != nil:
		h.log.WithError(err).WithField("consumption_id", id).Error("Failed to correct consumption")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to correct consumption"})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"consumption": res.Correction, "superseded": res.Superseded, "stock_movements": res.Movements})
}

// PrintJobCompletionRequest is what an operator reports when a print job
// finished. Every field is optional.
type PrintJobCompletionRequest struct {
	MaterialQuantity *float64 `json:"material_quantity"`
	MaterialUnit     string   `json:"material_unit"`
	MaterialCode     string   `json:"material_code"`
	Disposition      string   `json:"disposition"`
}

// CompletePrintJob is POST /v1/print-jobs/:id/complete.
// @Summary Mark a print job finished and record its actual consumption
// @Description An operator marks a print job finished (status completed, completed_at now unless set): a record only, nothing is sent to any machine. Its consumption is then recorded (ConsumptionService.RecordPrintJob): slicer estimates, printer-reported actuals already on the job, machine time, and, when material_quantity is given, the operator's actual material (material_unit g, mm, ml or sheet; material_code defaults to the print profile's material type). disposition (good, scrap, failed_print) applies to the material recorded. Calling it again on a completed job records nothing new. A cancelled or failed print job is refused (409 print_job_closed).
// @Tags jobs
// @Accept json
// @Produce json
// @Param id path string true "Print job ID (UUID)"
// @Param body body PrintJobCompletionRequest false "Operator's report"
// @Success 200 {object} map[string]interface{} "{print_job_id, marked_completed, consumption, created, stock_movements, material_recorded}"
// @Failure 400 {object} map[string]string "invalid_id or validation_error"
// @Failure 404 {object} map[string]string "print_job_not_found"
// @Failure 409 {object} map[string]string "print_job_closed"
// @Security BearerAuth
// @Router /print-jobs/{id}/complete [post]
func (h *ConsumptionHandler) CompletePrintJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		badRequest(c, "invalid_id", "id is not a UUID")
		return
	}
	rawTenant, _ := middleware.GetTenantID(c)
	tenantID, err := uuid.Parse(rawTenant)
	if err != nil {
		c.JSON(http.StatusForbidden, gin.H{"error": "forbidden", "message": "no tenant context"})
		return
	}
	var req PrintJobCompletionRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			badRequest(c, "validation_error", err.Error())
			return
		}
	}
	if len(req.MaterialCode) > materialFieldMaxLen {
		badRequest(c, "validation_error", "material_code is at most 100 characters")
		return
	}
	in := services.PrintJobCompletion{MaterialQuantity: req.MaterialQuantity, MaterialUnit: req.MaterialUnit,
		MaterialCode: req.MaterialCode, Disposition: req.Disposition}
	if a := middleware.ActorUUID(c); a != uuid.Nil {
		in.By = &a
	}
	res, err := h.svc.CompletePrintJob(c.Request.Context(), tenantID, id, in)
	switch {
	case errors.Is(err, services.ErrConsumptionPrintJobNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "print_job_not_found", "message": "Print job not found"})
		return
	case errors.Is(err, services.ErrConsumptionPrintJobClosed):
		c.JSON(http.StatusConflict, gin.H{"error": "print_job_closed", "message": err.Error()})
		return
	case errors.Is(err, services.ErrConsumptionInvalid):
		badRequest(c, "validation_error", err.Error())
		return
	case err != nil:
		h.log.WithError(err).WithField("print_job_id", id).Error("Failed to complete print job")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal_error", "message": "Failed to complete print job"})
		return
	}
	facts := res.Facts
	if facts == nil {
		facts = []repositories.JobConsumption{}
	}
	c.JSON(http.StatusOK, gin.H{"print_job_id": res.PrintJobID, "marked_completed": res.MarkedCompleted,
		"consumption": facts, "created": res.Created, "stock_movements": res.Movements,
		"material_recorded": res.MaterialRecorded})
}
