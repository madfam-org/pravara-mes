// Package api provides HTTP handlers for the machine adapter service.
package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/machine-adapter/internal/registry"
)

// RegistryHandlers provides REST endpoints for dynamic machine registration.
type RegistryHandlers struct {
	reg *registry.Registry
	log *logrus.Logger
}

// NewRegistryHandlers creates new registry handlers.
func NewRegistryHandlers(reg *registry.Registry, log *logrus.Logger) *RegistryHandlers {
	return &RegistryHandlers{reg: reg, log: log}
}

// RegisterRoutes adds the registry management routes to the given router group.
func (h *RegistryHandlers) RegisterRoutes(api *gin.RouterGroup) {
	api.POST("/definitions", h.Create)
	api.PUT("/definitions/:id", h.Update)
	api.DELETE("/definitions/:id", h.Delete)
}

// Create registers a new machine definition at runtime.
func (h *RegistryHandlers) Create(c *gin.Context) {
	var def registry.MachineDefinition
	if err := c.ShouldBindJSON(&def); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	id := c.Query("id")
	if id == "" {
		// Generate an ID from manufacturer + model
		id = sanitizeID(def.Manufacturer, def.Model)
	}

	if def.ID == uuid.Nil {
		def.ID = uuid.New()
	}

	// Check if already exists
	if _, exists := h.reg.GetDefinition(id); exists {
		c.JSON(http.StatusConflict, gin.H{"error": "definition already exists", "id": id})
		return
	}

	h.reg.RegisterDefinition(id, &def)

	// Persist to DB for reload on restart
	if err := h.reg.PersistDefinition(id, &def); err != nil {
		h.log.WithError(err).Warn("Failed to persist definition to database")
	}

	h.log.WithFields(logrus.Fields{
		"id":           id,
		"manufacturer": def.Manufacturer,
		"model":        def.Model,
	}).Info("Machine definition registered")

	c.JSON(http.StatusCreated, gin.H{
		"id":         id,
		"definition": def,
	})
}

// Update replaces an existing machine definition.
func (h *RegistryHandlers) Update(c *gin.Context) {
	id := c.Param("id")

	if _, exists := h.reg.GetDefinition(id); !exists {
		c.JSON(http.StatusNotFound, gin.H{"error": "definition not found"})
		return
	}

	var def registry.MachineDefinition
	if err := c.ShouldBindJSON(&def); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if def.ID == uuid.Nil {
		def.ID = uuid.New()
	}

	h.reg.RegisterDefinition(id, &def)

	if err := h.reg.PersistDefinition(id, &def); err != nil {
		h.log.WithError(err).Warn("Failed to persist updated definition")
	}

	h.log.WithField("id", id).Info("Machine definition updated")
	c.JSON(http.StatusOK, gin.H{
		"id":         id,
		"definition": def,
	})
}

// Delete removes a machine definition.
func (h *RegistryHandlers) Delete(c *gin.Context) {
	id := c.Param("id")

	if !h.reg.DeleteDefinition(id) {
		c.JSON(http.StatusNotFound, gin.H{"error": "definition not found"})
		return
	}

	if err := h.reg.DeletePersistedDefinition(id); err != nil {
		h.log.WithError(err).Warn("Failed to delete persisted definition")
	}

	h.log.WithField("id", id).Info("Machine definition deleted")
	c.JSON(http.StatusNoContent, nil)
}

// sanitizeID creates a registry ID from manufacturer and model.
func sanitizeID(manufacturer, model string) string {
	result := make([]byte, 0, len(manufacturer)+1+len(model))
	for _, s := range []string{manufacturer, model} {
		for _, c := range s {
			if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
				result = append(result, byte(c))
			} else if c >= 'A' && c <= 'Z' {
				result = append(result, byte(c-'A'+'a'))
			} else if c == ' ' || c == '-' {
				result = append(result, '_')
			}
		}
		if s == manufacturer {
			result = append(result, '_')
		}
	}
	return string(result)
}
