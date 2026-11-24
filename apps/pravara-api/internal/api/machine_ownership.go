package api

import (
	"net/http"
	"strings"

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

// tenantRef is one id a request body references; a nil ID is not checked.
type tenantRef struct {
	ref repositories.Ref
	id  *uuid.UUID
}

// refLabels name each reference in the 404 message.
var refLabels = map[repositories.Ref]string{
	repositories.RefMachines:            "Machine",
	repositories.RefUsers:               "User",
	repositories.RefOrders:              "Order",
	repositories.RefOrderItems:          "Order item",
	repositories.RefTasks:               "Task",
	repositories.RefMaintenanceSchedule: "Maintenance schedule",
	repositories.RefBatchLots:           "Batch lot",
	repositories.RefCertificates:        "Certificate",
	repositories.RefInspections:         "Inspection",
	repositories.RefProductDefinitions:  "Product definition",
}

// requireTenantRefs checks, before a handler writes, that every non-nil id
// in refs names a row of the request's tenant, reading in the request's
// tenant scope. The first id that is not found answers 404 (500 when the
// lookup fails) and the caller returns on false.
func requireTenantRefs(c *gin.Context, q repositories.DBTX, log *logrus.Logger, refs ...tenantRef) bool {
	for _, r := range refs {
		if r.id == nil {
			continue
		}
		label := refLabels[r.ref]
		if q == nil {
			log.WithField("reference", r.ref).Error("Reference lookup is not configured")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to retrieve " + strings.ToLower(label),
			})
			return false
		}
		ok, err := repositories.ReferenceExists(c.Request.Context(), q, r.ref, *r.id)
		if err != nil {
			log.WithError(err).WithField("reference", r.ref).Error("Failed to look up referenced row")
			c.JSON(http.StatusInternalServerError, gin.H{
				"error":   "internal_error",
				"message": "Failed to retrieve " + strings.ToLower(label),
			})
			return false
		}
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": label + " not found: " + r.id.String(),
			})
			return false
		}
	}
	return true
}

// ref builds a tenantRef for a required (non-pointer) id.
func ref(r repositories.Ref, id uuid.UUID) tenantRef {
	return tenantRef{ref: r, id: &id}
}

// optRef builds a tenantRef for an optional id.
func optRef(r repositories.Ref, id *uuid.UUID) tenantRef {
	return tenantRef{ref: r, id: id}
}
