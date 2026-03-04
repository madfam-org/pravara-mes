package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/models"
	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/physics"
	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/renderer"
	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/storage"
	"github.com/madfam-org/pravara-mes/apps/visualization-engine/internal/yantra4d"
)

var (
	log            *logrus.Logger
	allowedOrigins []string
	upgrader       = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // Same-origin requests
			}
			for _, allowed := range allowedOrigins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
)

func init() {
	log = logrus.New()
	log.SetFormatter(&logrus.JSONFormatter{})
	log.SetOutput(os.Stdout)

	// Load configuration
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(".")
	viper.AddConfigPath("./config")
	viper.SetEnvPrefix("VIZENGINE")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("server.port", "4502")
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.name", "pravara")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("cors.allowed_origins", "https://pravara.madfam.io")
	viper.SetDefault("storage.endpoint", "")
	viper.SetDefault("storage.bucket", "pravara-models")
	viper.SetDefault("storage.access_key", "")
	viper.SetDefault("storage.secret_key", "")
	viper.SetDefault("storage.region", "auto")

	// Yantra4D integration
	viper.SetDefault("yantra4d.base_url", "https://yantra4d.madfam.io")
	viper.SetDefault("yantra4d.timeout", 120)
}

func main() {
	// Load config
	if err := viper.ReadInConfig(); err != nil {
		log.Warnf("No config file found, using environment variables: %v", err)
	}

	// Parse allowed CORS origins
	originsStr := viper.GetString("cors.allowed_origins")
	for _, o := range strings.Split(originsStr, ",") {
		o = strings.TrimSpace(o)
		if o != "" {
			allowedOrigins = append(allowedOrigins, o)
		}
	}

	// Connect to database
	db, err := connectDB()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Connect to Redis
	rdb := connectRedis()
	defer rdb.Close()

	// Initialize S3 storage (optional - model upload works only if configured)
	var storageClient *storage.Client
	if endpoint := viper.GetString("storage.endpoint"); endpoint != "" {
		var storageErr error
		storageClient, storageErr = storage.NewClient(storage.Config{
			Endpoint:  endpoint,
			Bucket:    viper.GetString("storage.bucket"),
			AccessKey: viper.GetString("storage.access_key"),
			SecretKey: viper.GetString("storage.secret_key"),
			Region:    viper.GetString("storage.region"),
		}, log)
		if storageErr != nil {
			log.Warnf("S3 storage not available (model upload disabled): %v", storageErr)
		} else {
			log.Info("S3 storage initialized for model uploads")
		}
	}

	// Initialize Yantra4D client
	y4dClient := yantra4d.NewClient(
		viper.GetString("yantra4d.base_url"),
		time.Duration(viper.GetInt("yantra4d.timeout"))*time.Second,
	)

	// Initialize Yantra4D importer (requires S3 storage)
	var y4dImporter *models.Yantra4DImporter
	if storageClient != nil {
		y4dImporter = models.NewYantra4DImporter(y4dClient, storageClient, log)
		log.Info("Yantra4D importer initialized")
	} else {
		log.Warn("Yantra4D importer disabled: S3 storage not configured")
	}

	// Initialize services
	modelManager := models.NewManager(db, log)
	physicsEngine := physics.NewEngine(log)
	renderService := renderer.NewService(log)

	// Setup Gin router
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	// CORS middleware
	router.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		allowed := false
		for _, o := range allowedOrigins {
			if origin == o {
				allowed = true
				break
			}
		}
		if allowed {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, Authorization")
			c.Writer.Header().Set("Vary", "Origin")
		}

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})

	// API routes
	v1 := router.Group("/v1")
	{
		// Model management
		v1.GET("/models", handleListModels(modelManager))
		v1.GET("/models/:id", handleGetModel(modelManager))
		v1.POST("/models", handleCreateModel(modelManager))
		v1.PUT("/models/:id", handleUpdateModel(modelManager))
		v1.DELETE("/models/:id", handleDeleteModel(modelManager))

		// Model upload (requires S3 storage)
		v1.POST("/models/upload", handleModelUpload(modelManager, storageClient))

		// Yantra4D import endpoint
		v1.POST("/models/import/yantra4d", handleYantra4DImport(modelManager, y4dImporter))

		// Factory layouts
		v1.GET("/layouts", handleListLayouts(modelManager))
		v1.GET("/layouts/:id", handleGetLayout(modelManager))
		v1.POST("/layouts", handleCreateLayout(modelManager))
		v1.PUT("/layouts/:id", handleUpdateLayout(modelManager))

		// Physics simulation
		v1.POST("/simulate/gcode", handleSimulateGCode(physicsEngine))
		v1.POST("/simulate/collision", handleCollisionCheck(physicsEngine))
		v1.POST("/simulate/material", handleMaterialSimulation(physicsEngine))

		// WebSocket for real-time updates
		v1.GET("/ws", handleWebSocket(renderService, rdb))
	}

	// Start telemetry subscriber
	go subscribeTelemetry(rdb, renderService)

	// Start server
	port := viper.GetString("server.port")
	srv := &http.Server{
		Addr:    ":" + port,
		Handler: router,
	}

	// Graceful shutdown
	go func() {
		sigint := make(chan os.Signal, 1)
		signal.Notify(sigint, os.Interrupt, syscall.SIGTERM)
		<-sigint

		log.Info("Shutting down server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("Server forced to shutdown: %v", err)
		}
	}()

	log.Infof("Visualization engine starting on port %s", port)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("Server failed to start: %v", err)
	}
}

func connectDB() (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		viper.GetString("database.host"),
		viper.GetInt("database.port"),
		viper.GetString("database.user"),
		viper.GetString("database.password"),
		viper.GetString("database.name"),
		viper.GetString("database.sslmode"),
	)

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}

func connectRedis() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", viper.GetString("redis.host"), viper.GetInt("redis.port")),
		Password: viper.GetString("redis.password"),
		DB:       viper.GetInt("redis.db"),
	})
}

// Telemetry subscription for real-time position updates
func subscribeTelemetry(rdb *redis.Client, renderService *renderer.Service) {
	ctx := context.Background()
	pubsub := rdb.Subscribe(ctx, "telemetry:*")
	defer pubsub.Close()

	ch := pubsub.Channel()
	for msg := range ch {
		// Parse telemetry and update 3D positions
		renderService.UpdateMachinePosition(msg.Payload)
	}
}

// Handler functions
func handleListModels(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		models, err := manager.ListModels(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, models)
	}
}

func handleGetModel(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		model, err := manager.GetModel(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, model)
	}
}

func handleCreateModel(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var model models.MachineModel
		if err := c.ShouldBindJSON(&model); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.CreateModel(c.Request.Context(), &model); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, model)
	}
}

func handleUpdateModel(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var model models.MachineModel
		if err := c.ShouldBindJSON(&model); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.UpdateModel(c.Request.Context(), c.Param("id"), &model); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, model)
	}
}

func handleDeleteModel(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := manager.DeleteModel(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusNoContent, nil)
	}
}

func handleListLayouts(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		layouts, err := manager.ListLayouts(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, layouts)
	}
}

func handleGetLayout(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		layout, err := manager.GetLayout(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, layout)
	}
}

func handleCreateLayout(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var layout models.FactoryLayout
		if err := c.ShouldBindJSON(&layout); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.CreateLayout(c.Request.Context(), &layout); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, layout)
	}
}

func handleUpdateLayout(manager *models.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var layout models.FactoryLayout
		if err := c.ShouldBindJSON(&layout); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.UpdateLayout(c.Request.Context(), c.Param("id"), &layout); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, layout)
	}
}

func handleSimulateGCode(engine *physics.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req physics.GCodeSimulationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result, err := engine.SimulateGCode(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func handleCollisionCheck(engine *physics.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req physics.CollisionCheckRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result, err := engine.CheckCollisions(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func handleMaterialSimulation(engine *physics.Engine) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req physics.MaterialSimulationRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		result, err := engine.SimulateMaterial(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, result)
	}
}

func handleModelUpload(manager *models.Manager, storageClient *storage.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		if storageClient == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "S3 storage not configured"})
			return
		}

		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file required"})
			return
		}
		defer file.Close()

		// Validate file type
		contentType, err := storage.ValidateModelFile(header.Filename)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Generate storage key
		key := fmt.Sprintf("models/%d_%s", time.Now().UnixNano(), header.Filename)

		// Upload to S3
		s3URL, err := storageClient.UploadModel(c.Request.Context(), key, file, header.Size, contentType)
		if err != nil {
			log.WithError(err).Error("Failed to upload model to S3")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "upload failed"})
			return
		}

		// Generate presigned download URL
		downloadURL, err := storageClient.GetPresignedURL(c.Request.Context(), key, 24*time.Hour)
		if err != nil {
			log.WithError(err).Warn("Failed to generate presigned URL, using S3 URI")
			downloadURL = s3URL
		}

		// Determine machine type from form or default
		machineType := c.PostForm("machine_type")
		if machineType == "" {
			machineType = "generic"
		}
		modelName := c.PostForm("name")
		if modelName == "" {
			modelName = header.Filename
		}

		// Create DB record
		model := &models.MachineModel{
			MachineType: machineType,
			Name:        modelName,
			ModelURL:    downloadURL,
			Scale:       1.0,
		}

		if err := manager.CreateModel(c.Request.Context(), model); err != nil {
			log.WithError(err).Error("Failed to create model record")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save model metadata"})
			return
		}

		c.JSON(http.StatusCreated, model)
	}
}

func handleYantra4DImport(manager *models.Manager, importer *models.Yantra4DImporter) gin.HandlerFunc {
	return func(c *gin.Context) {
		if importer == nil || !importer.IsConfigured() {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "Yantra4D importer not configured"})
			return
		}

		var req models.Yantra4DImportRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Extract JWT from Authorization header
		jwt := c.GetHeader("Authorization")
		if len(jwt) > 7 && jwt[:7] == "Bearer " {
			jwt = jwt[7:]
		} else {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Bearer token required"})
			return
		}

		opts := models.ImportOptions{
			MachineType: req.MachineType,
			Metadata:    map[string]string{"jwt": jwt},
		}

		model, err := importer.ImportFull(c.Request.Context(), req.Slug, req.Params, jwt, opts)
		if err != nil {
			log.WithError(err).Error("Yantra4D import failed")
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("import failed: %v", err)})
			return
		}

		if err := manager.CreateModel(c.Request.Context(), model); err != nil {
			log.WithError(err).Error("Failed to save imported model")
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save model"})
			return
		}

		c.JSON(http.StatusCreated, model)
	}
}

func handleWebSocket(renderService *renderer.Service, rdb *redis.Client) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Errorf("Failed to upgrade WebSocket: %v", err)
			return
		}
		defer conn.Close()

		// Handle WebSocket connection for real-time 3D updates
		renderService.HandleWebSocket(conn, rdb)
	}
}