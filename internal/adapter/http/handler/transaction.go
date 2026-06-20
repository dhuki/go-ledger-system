package handler

import (
	"net/http"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
	"github.com/dhuki/go-ledger-system/internal/adapter/http/model"
	transfer "github.com/dhuki/go-ledger-system/internal/core/transaction"
	"github.com/gin-gonic/gin"
)

type transactionHandler struct {
	service transfer.Service
}

func NewTransactionHandler(service transfer.Service) *transactionHandler {
	return &transactionHandler{service: service}
}

func (h *transactionHandler) RegisterRoute(g *gin.RouterGroup) {
	transferGroup := g.Group("/transfer")
	transferGroup.POST("", middleware.WithLogReqBody(), h.CreateTransfer)
}

func (h *transactionHandler) CreateTransfer(c *gin.Context) {
	ctx := c.Request.Context()

	var req model.CreateTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, model.BaseResponse{
			Status:  http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	if err := req.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, model.BaseResponse{
			Status:  http.StatusBadRequest,
			Message: err.Error(),
		})
		return
	}

	resp, err := h.service.CreateTransfer(ctx, &req)
	if err != nil {
		status := mapErrorToStatus(err)
		c.JSON(status, model.BaseResponse{
			Status:  status,
			Message: err.Error(),
		})
		return
	}

	c.JSON(http.StatusCreated, model.BaseResponse{
		Status:  http.StatusOK,
		Message: "success",
		Data:    resp,
	})
}
