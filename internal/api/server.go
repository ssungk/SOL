package api

import (
	"sol/internal/sol"

	"github.com/gin-gonic/gin"
)

// Server represents the API server
type Server struct {
	router    *gin.Engine
	port      string
	solServer *sol.Server // DI된 sol 서버
}

// NewServerWithDI creates a new API server instance with dependency injection
func NewServerWithDI(port string, solServer *sol.Server) *Server {
	// Set Gin to release mode for production
	gin.SetMode(gin.ReleaseMode)
	
	router := gin.New()
	
	// Add basic middleware
	router.Use(gin.Logger())
	router.Use(gin.Recovery())
	
	return &Server{
		router:    router,
		port:      port,
		solServer: solServer,
	}
}

// NewServer creates a new API server instance (기존 호환성 유지)
func NewServer(port string) *Server {
	return NewServerWithDI(port, nil)
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
	return s.router.Run(":" + s.port)
}

// GetRouter returns the gin router (for testing)
func (s *Server) GetRouter() *gin.Engine {
	return s.router
}
