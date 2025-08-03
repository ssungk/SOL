package api

import (
	"log/slog"
	"sol/internal/media"

	"github.com/gin-gonic/gin"
)

// Server represents the API server
type Server struct {
	router    *gin.Engine
	port      string
	mediaServer *media.MediaServer // DI된 media 서버
}

// NewServer creates a new API server instance
func NewServer(port string, mediaServer *media.MediaServer) *Server {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	router := gin.New()
	
	// Add basic middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	
	return &Server{
		router:      router,
		port:        port,
		mediaServer: mediaServer,
	}
}

// SetupRoutes configures all API routes
func (s *Server) SetupRoutes() {
	// API v1 group
	v1 := s.router.Group("/api/v1")
	{
		v1.POST("/pull", s.PullHandler)
	}
}

// Start starts the API server
func (s *Server) Start() error {
	s.SetupRoutes()
	
	// 논블로킹으로 서버 시작
	go func() {
		if err := s.router.Run(":" + s.port); err != nil {
			slog.Error("API server error", "err", err)
		}
	}()
	
	return nil
}

// GetRouter returns the gin router (for testing)
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}
