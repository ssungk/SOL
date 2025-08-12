package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// PullRequest represents the request body for pull endpoint
type PullRequest struct {
	// 향후 pull 기능 요구사항에 따라 필드 추가 예정
}

// PullResponse represents the response body for pull endpoint
type PullResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// PullHandler handles POST /api/v1/pull requests
func (s *Server) PullHandler(c *gin.Context) {
	var req PullRequest
	
	// Parse request body (optional for now)
	if err := c.ShouldBindJSON(&req); err != nil {
		// For now, ignore binding errors and always return 200
	}

	// MediaServer 연동 기능은 필요에 따라 구현
	// 예: 스트림 목록 조회, 통계 정보 등

	// Always return 200 OK as requested
	response := PullResponse{
		Success: true,
		Message: "Pull request processed successfully",
	}

	c.JSON(http.StatusOK, response)
}
