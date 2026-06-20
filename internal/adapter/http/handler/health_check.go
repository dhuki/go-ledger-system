package handler

import (
	"net/http"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
	healthcheck "github.com/dhuki/go-ledger-system/internal/core/healthcheck"
	"github.com/gin-gonic/gin"
)

type healthcheckHandler struct {
	service healthcheck.Service
}

func NewHealthCheckHandler(service healthcheck.Service) *healthcheckHandler {
	return &healthcheckHandler{service: service}
}

func (h *healthcheckHandler) RegisterRoute(g *gin.RouterGroup) {
	healthGroup := g.Group("/health")
	healthGroup.GET("", h.CheckHealth, middleware.WithLogReqBody())
	healthGroup.POST("/echo", middleware.WithLogReqBody(), h.Echo)
}

func (h *healthcheckHandler) Echo(c *gin.Context) {
	var payload any
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, payload)
}

func (h *healthcheckHandler) CheckHealth(c *gin.Context) {
	err := h.service.HealthCheck(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "health check passed"})
}
