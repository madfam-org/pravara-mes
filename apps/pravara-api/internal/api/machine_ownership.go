package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
)

// requireTenantMachine checks that machineID names a machine of the
// request's tenant before a handler writes a row that references it. The
// machine repository reads within the request's tenant scope, so a machine
// of another tenant reads as not found. On false the response (404, or 500
// when the read fails) has been written and the caller returns.
func requireTenantMachine(c *gin.Context, machines *repositories.MachineRepository, log *logrus.Logger, machineID uuid.UUID) bool {
	machine, err := machines.GetByID(c.Request.Context(), machineID)
	if err != nil {
		log.WithError(err).WithField("machine_id", machineID).Error("Failed to look up machine")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve machine",
		})
		return false
	}
	if machine == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Machine not found: " + machineID.String(),
		})
		return false
	}
	return true
}
