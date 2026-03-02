package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	_ "github.com/lib/pq"
	"github.com/pion/webrtc/v3"
	"github.com/redis/go-redis/v9"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"

	"github.com/madfam-org/pravara-mes/apps/video-streaming/internal/camera"
	"github.com/madfam-org/pravara-mes/apps/video-streaming/internal/recording"
	"github.com/madfam-org/pravara-mes/apps/video-streaming/internal/rtc"
)

var (
	log      *logrus.Logger
	upgrader = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			// TODO: Implement proper CORS checking
			return true
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
	viper.SetEnvPrefix("VIDEO")
	viper.AutomaticEnv()

	// Set defaults
	viper.SetDefault("server.port", "4503")
	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.name", "pravara")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("webrtc.stun_servers", []string{"stun:stun.l.google.com:19302"})
}

func main() {
	// Load config
	if err := viper.ReadInConfig(); err != nil {
		log.Warnf("No config file found, using environment variables: %v", err)
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

	// Initialize services
	cameraManager := camera.NewManager(db, log)
	rtcManager := rtc.NewManager(log)
	recordingService := recording.NewService(db, log)

	// Initialize WebRTC configuration
	webrtcConfig := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: viper.GetStringSlice("webrtc.stun_servers"),
			},
		},
	}
	rtcManager.SetConfig(webrtcConfig)

	// Setup Gin router
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(gin.Logger())

	// CORS middleware for WebRTC
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS, PUT, DELETE")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

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
		// Camera management
		v1.GET("/cameras", handleListCameras(cameraManager))
		v1.GET("/cameras/:id", handleGetCamera(cameraManager))
		v1.POST("/cameras", handleCreateCamera(cameraManager))
		v1.PUT("/cameras/:id", handleUpdateCamera(cameraManager))
		v1.DELETE("/cameras/:id", handleDeleteCamera(cameraManager))
		v1.POST("/cameras/:id/discover", handleDiscoverCameras(cameraManager))

		// WebRTC signaling
		v1.POST("/rtc/offer", handleRTCOffer(rtcManager))
		v1.POST("/rtc/answer", handleRTCAnswer(rtcManager))
		v1.POST("/rtc/ice", handleICECandidate(rtcManager))
		v1.GET("/rtc/ws", handleWebRTCSignaling(rtcManager))

		// Recording management
		v1.POST("/recordings/start", handleStartRecording(recordingService))
		v1.POST("/recordings/stop", handleStopRecording(recordingService))
		v1.GET("/recordings", handleListRecordings(recordingService))
		v1.GET("/recordings/:id", handleGetRecording(recordingService))
		v1.DELETE("/recordings/:id", handleDeleteRecording(recordingService))

		// Live streams
		v1.GET("/streams", handleListStreams(rtcManager))
		v1.GET("/streams/:camera_id/ws", handleStreamWebSocket(rtcManager, cameraManager))
	}

	// Start camera discovery service
	go cameraManager.StartDiscovery()

	// Start stream monitoring
	go rtcManager.MonitorStreams()

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

		log.Info("Shutting down video streaming server...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		rtcManager.CloseAllConnections()
		recordingService.StopAllRecordings()

		if err := srv.Shutdown(ctx); err != nil {
			log.Fatalf("Server forced to shutdown: %v", err)
		}
	}()

	log.Infof("Video streaming server starting on port %s", port)
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

// Handler functions
func handleListCameras(manager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cameras, err := manager.ListCameras(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, cameras)
	}
}

func handleGetCamera(manager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		camera, err := manager.GetCamera(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, camera)
	}
}

func handleCreateCamera(manager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cam camera.Camera
		if err := c.ShouldBindJSON(&cam); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.CreateCamera(c.Request.Context(), &cam); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusCreated, cam)
	}
}

func handleUpdateCamera(manager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var cam camera.Camera
		if err := c.ShouldBindJSON(&cam); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.UpdateCamera(c.Request.Context(), c.Param("id"), &cam); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, cam)
	}
}

func handleDeleteCamera(manager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := manager.DeleteCamera(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusNoContent, nil)
	}
}

func handleDiscoverCameras(manager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		discovered, err := manager.DiscoverCameras(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"discovered": discovered})
	}
}

func handleRTCOffer(manager *rtc.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req rtc.OfferRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		answer, err := manager.HandleOffer(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, answer)
	}
}

func handleRTCAnswer(manager *rtc.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req rtc.AnswerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.HandleAnswer(c.Request.Context(), req); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func handleICECandidate(manager *rtc.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req rtc.ICECandidateRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := manager.HandleICECandidate(c.Request.Context(), req); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func handleWebRTCSignaling(manager *rtc.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Errorf("Failed to upgrade WebSocket: %v", err)
			return
		}
		defer conn.Close()

		// Handle WebRTC signaling over WebSocket
		manager.HandleSignalingWebSocket(conn)
	}
}

func handleStartRecording(service *recording.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req recording.StartRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		recordingID, err := service.StartRecording(c.Request.Context(), req)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"recording_id": recordingID})
	}
}

func handleStopRecording(service *recording.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req recording.StopRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := service.StopRecording(c.Request.Context(), req.RecordingID); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "stopped"})
	}
}

func handleListRecordings(service *recording.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		recordings, err := service.ListRecordings(c.Request.Context())
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, recordings)
	}
}

func handleGetRecording(service *recording.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		recording, err := service.GetRecording(c.Request.Context(), c.Param("id"))
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, recording)
	}
}

func handleDeleteRecording(service *recording.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := service.DeleteRecording(c.Request.Context(), c.Param("id")); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusNoContent, nil)
	}
}

func handleListStreams(manager *rtc.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		streams := manager.ListActiveStreams()
		c.JSON(http.StatusOK, streams)
	}
}

func handleStreamWebSocket(rtcManager *rtc.Manager, camManager *camera.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		cameraID := c.Param("camera_id")

		// Get camera details
		cam, err := camManager.GetCamera(c.Request.Context(), cameraID)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Camera not found"})
			return
		}

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Errorf("Failed to upgrade WebSocket: %v", err)
			return
		}
		defer conn.Close()

		// Create WebRTC peer connection for this camera stream
		rtcManager.HandleCameraStream(conn, cam)
	}
}