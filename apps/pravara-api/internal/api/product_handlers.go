package api

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/db/repositories"
	"github.com/madfam-org/pravara-mes/apps/pravara-api/internal/middleware"
)

// ProductHandler handles product definition HTTP requests.
type ProductHandler struct {
	productRepo *repositories.ProductRepository
	log         *logrus.Logger
}

// NewProductHandler creates a new product handler.
func NewProductHandler(productRepo *repositories.ProductRepository, log *logrus.Logger) *ProductHandler {
	return &ProductHandler{
		productRepo: productRepo,
		log:         log,
	}
}

// CreateProductRequest represents the request body for creating a product definition.
type CreateProductRequest struct {
	SKU             string         `json:"sku" binding:"required"`
	Name            string         `json:"name" binding:"required"`
	Version         string         `json:"version" binding:"required"`
	Category        string         `json:"category" binding:"required"`
	Description     string         `json:"description"`
	CADFileURL      string         `json:"cad_file_url"`
	ParametricSpecs map[string]any `json:"parametric_specs"`
	IsActive        *bool          `json:"is_active"`
	Metadata        map[string]any `json:"metadata"`
}

// UpdateProductRequest represents the request body for updating a product definition.
type UpdateProductRequest struct {
	Name            string         `json:"name"`
	Version         string         `json:"version"`
	Category        string         `json:"category"`
	Description     string         `json:"description"`
	CADFileURL      string         `json:"cad_file_url"`
	ParametricSpecs map[string]any `json:"parametric_specs"`
	IsActive        *bool          `json:"is_active"`
	Metadata        map[string]any `json:"metadata"`
}

// CreateBOMItemRequest represents the request body for creating a BOM item.
type CreateBOMItemRequest struct {
	MaterialName  string   `json:"material_name" binding:"required"`
	MaterialCode  string   `json:"material_code"`
	Quantity      float64  `json:"quantity" binding:"required"`
	Unit          string   `json:"unit" binding:"required"`
	EstimatedCost *float64 `json:"estimated_cost"`
	Currency      string   `json:"currency"`
	Supplier      string   `json:"supplier"`
	SortOrder     int      `json:"sort_order"`
}

// ListProducts returns a paginated list of product definitions.
// @Summary List product definitions
// @Description Returns a paginated list of product definitions with optional filtering
// @Tags products
// @Produce json
// @Param limit query int false "Number of results per page" default(20)
// @Param offset query int false "Offset for pagination" default(0)
// @Param category query string false "Filter by category"
// @Param is_active query bool false "Filter by active status"
// @Param search query string false "Search by SKU or name"
// @Success 200 {object} ListResponse "Paginated product list"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products [get]
func (h *ProductHandler) ListProducts(c *gin.Context) {
	filter := repositories.ProductFilter{
		Limit:  20,
		Offset: 0,
	}

	if limit, err := strconv.Atoi(c.DefaultQuery("limit", "20")); err == nil && limit > 0 {
		filter.Limit = limit
	}
	if offset, err := strconv.Atoi(c.DefaultQuery("offset", "0")); err == nil && offset >= 0 {
		filter.Offset = offset
	}

	if category := c.Query("category"); category != "" {
		filter.Category = &category
	}

	if isActive := c.Query("is_active"); isActive != "" {
		if val, err := strconv.ParseBool(isActive); err == nil {
			filter.IsActive = &val
		}
	}

	if search := c.Query("search"); search != "" {
		filter.Search = &search
	}

	products, total, err := h.productRepo.List(c.Request.Context(), filter)
	if err != nil {
		h.log.WithError(err).Error("Failed to list product definitions")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve product definitions",
		})
		return
	}

	c.JSON(http.StatusOK, ListResponse{
		Data:   products,
		Total:  total,
		Limit:  filter.Limit,
		Offset: filter.Offset,
	})
}

// GetProductByID returns a single product definition by ID.
// @Summary Get product definition by ID
// @Description Returns a single product definition with all details
// @Tags products
// @Produce json
// @Param id path string true "Product Definition ID (UUID)"
// @Success 200 {object} repositories.ProductDefinition "Product definition details"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products/{id} [get]
func (h *ProductHandler) GetProductByID(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid product ID format",
		})
		return
	}

	product, err := h.productRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve product definition",
		})
		return
	}

	if product == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Product definition not found",
		})
		return
	}

	c.JSON(http.StatusOK, product)
}

// CreateProduct creates a new product definition.
// @Summary Create a new product definition
// @Description Creates a new product definition with BOM support
// @Tags products
// @Accept json
// @Produce json
// @Param body body CreateProductRequest true "Product definition data"
// @Success 201 {object} repositories.ProductDefinition "Created product definition"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products [post]
func (h *ProductHandler) CreateProduct(c *gin.Context) {
	var req CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	tenantUUID, _ := uuid.Parse(tenantID)

	product := &repositories.ProductDefinition{
		TenantID:        tenantUUID,
		SKU:             req.SKU,
		Name:            req.Name,
		Version:         req.Version,
		Category:        req.Category,
		Description:     req.Description,
		CADFileURL:      req.CADFileURL,
		ParametricSpecs: req.ParametricSpecs,
		IsActive:        true,
		Metadata:        req.Metadata,
	}

	if req.IsActive != nil {
		product.IsActive = *req.IsActive
	}

	if err := h.productRepo.Create(c.Request.Context(), product); err != nil {
		h.log.WithError(err).Error("Failed to create product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create product definition",
		})
		return
	}

	h.log.WithField("product_id", product.ID).Info("Product definition created")
	c.JSON(http.StatusCreated, product)
}

// UpdateProduct modifies an existing product definition.
// @Summary Update a product definition
// @Description Updates product definition fields
// @Tags products
// @Accept json
// @Produce json
// @Param id path string true "Product Definition ID (UUID)"
// @Param body body UpdateProductRequest true "Product definition update data"
// @Success 200 {object} repositories.ProductDefinition "Updated product definition"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products/{id} [put]
func (h *ProductHandler) UpdateProduct(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid product ID format",
		})
		return
	}

	product, err := h.productRepo.GetByID(c.Request.Context(), id)
	if err != nil {
		h.log.WithError(err).Error("Failed to get product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve product definition",
		})
		return
	}

	if product == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Product definition not found",
		})
		return
	}

	var req UpdateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	// Update fields
	if req.Name != "" {
		product.Name = req.Name
	}
	if req.Version != "" {
		product.Version = req.Version
	}
	if req.Category != "" {
		product.Category = req.Category
	}
	if req.Description != "" {
		product.Description = req.Description
	}
	if req.CADFileURL != "" {
		product.CADFileURL = req.CADFileURL
	}
	if req.ParametricSpecs != nil {
		product.ParametricSpecs = req.ParametricSpecs
	}
	if req.IsActive != nil {
		product.IsActive = *req.IsActive
	}
	if req.Metadata != nil {
		product.Metadata = req.Metadata
	}

	if err := h.productRepo.Update(c.Request.Context(), product); err != nil {
		h.log.WithError(err).Error("Failed to update product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to update product definition",
		})
		return
	}

	h.log.WithField("product_id", product.ID).Info("Product definition updated")
	c.JSON(http.StatusOK, product)
}

// DeleteProduct removes a product definition.
// @Summary Delete a product definition
// @Description Removes a product definition from the system
// @Tags products
// @Produce json
// @Param id path string true "Product Definition ID (UUID)"
// @Success 200 {object} map[string]string "Deletion confirmation"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products/{id} [delete]
func (h *ProductHandler) DeleteProduct(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid product ID format",
		})
		return
	}

	if err := h.productRepo.Delete(c.Request.Context(), id); err != nil {
		if err.Error() == "product definition not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "Product definition not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete product definition",
		})
		return
	}

	h.log.WithField("product_id", id).Info("Product definition deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "Product definition deleted successfully",
	})
}

// GetBOM returns the bill of materials for a product definition.
// @Summary Get product BOM
// @Description Returns all BOM items for a product definition
// @Tags products
// @Produce json
// @Param id path string true "Product Definition ID (UUID)"
// @Success 200 {object} map[string]interface{} "BOM items list"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products/{id}/bom [get]
func (h *ProductHandler) GetBOM(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid product ID format",
		})
		return
	}

	// Verify product exists
	product, err := h.productRepo.GetByID(c.Request.Context(), productID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve product definition",
		})
		return
	}
	if product == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Product definition not found",
		})
		return
	}

	items, err := h.productRepo.ListBOMItems(c.Request.Context(), productID)
	if err != nil {
		h.log.WithError(err).Error("Failed to list BOM items")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve BOM items",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"product_id": productID,
		"items":      items,
		"count":      len(items),
	})
}

// AddBOMItem adds a BOM item to a product definition.
// @Summary Add BOM item
// @Description Adds a new bill of materials item to a product definition
// @Tags products
// @Accept json
// @Produce json
// @Param id path string true "Product Definition ID (UUID)"
// @Param body body CreateBOMItemRequest true "BOM item data"
// @Success 201 {object} repositories.BOMItem "Created BOM item"
// @Failure 400 {object} map[string]string "Validation error"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 404 {object} map[string]string "Product not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products/{id}/bom/items [post]
func (h *ProductHandler) AddBOMItem(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid product ID format",
		})
		return
	}

	// Verify product exists
	product, err := h.productRepo.GetByID(c.Request.Context(), productID)
	if err != nil {
		h.log.WithError(err).Error("Failed to get product definition")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to retrieve product definition",
		})
		return
	}
	if product == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"error":   "not_found",
			"message": "Product definition not found",
		})
		return
	}

	var req CreateBOMItemRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "validation_error",
			"message": err.Error(),
		})
		return
	}

	tenantID, ok := middleware.GetTenantID(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error":   "unauthorized",
			"message": "Tenant context not found",
		})
		return
	}

	tenantUUID, _ := uuid.Parse(tenantID)

	item := &repositories.BOMItem{
		TenantID:            tenantUUID,
		ProductDefinitionID: productID,
		MaterialName:        req.MaterialName,
		MaterialCode:        req.MaterialCode,
		Quantity:            req.Quantity,
		Unit:                req.Unit,
		EstimatedCost:       req.EstimatedCost,
		Currency:            req.Currency,
		Supplier:            req.Supplier,
		SortOrder:           req.SortOrder,
	}

	if err := h.productRepo.CreateBOMItem(c.Request.Context(), item); err != nil {
		h.log.WithError(err).Error("Failed to create BOM item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to create BOM item",
		})
		return
	}

	h.log.WithFields(logrus.Fields{
		"product_id": productID,
		"item_id":    item.ID,
	}).Info("BOM item created")
	c.JSON(http.StatusCreated, item)
}

// DeleteBOMItem removes a BOM item from a product definition.
// @Summary Delete BOM item
// @Description Removes a BOM item from a product definition
// @Tags products
// @Produce json
// @Param id path string true "Product Definition ID (UUID)"
// @Param itemId path string true "BOM Item ID (UUID)"
// @Success 200 {object} map[string]string "Deletion confirmation"
// @Failure 400 {object} map[string]string "Invalid ID format"
// @Failure 404 {object} map[string]string "BOM item not found"
// @Failure 500 {object} map[string]string "Internal server error"
// @Security BearerAuth
// @Router /products/{id}/bom/items/{itemId} [delete]
func (h *ProductHandler) DeleteBOMItem(c *gin.Context) {
	_, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid product ID format",
		})
		return
	}

	itemID, err := uuid.Parse(c.Param("itemId"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   "invalid_id",
			"message": "Invalid BOM item ID format",
		})
		return
	}

	if err := h.productRepo.DeleteBOMItem(c.Request.Context(), itemID); err != nil {
		if err.Error() == "BOM item not found" {
			c.JSON(http.StatusNotFound, gin.H{
				"error":   "not_found",
				"message": "BOM item not found",
			})
			return
		}
		h.log.WithError(err).Error("Failed to delete BOM item")
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "internal_error",
			"message": "Failed to delete BOM item",
		})
		return
	}

	h.log.WithField("item_id", itemID).Info("BOM item deleted")
	c.JSON(http.StatusOK, gin.H{
		"message": "BOM item deleted successfully",
	})
}
