package middleware

import (
	"bytes"
	"context"
	"io"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"

	"github.com/dhuki/go-ledger-system/internal/infra/logger"
)

type contextKey string

const (
	ReqBodyKey     contextKey = "X-Req-Body"
	IdempotencyKey contextKey = "X-Idempotency-Key"
)

type requestLog struct {
	Timestamp    time.Time `json:"timestamp"`
	Method       string    `json:"method"`
	URL          string    `json:"url"`
	Status       int       `json:"status"`
	ResponseTime float64   `json:"response_time_ms"`
	ResponseSize int       `json:"response_size"`
	ReqBody      any       `json:"req_body,omitempty"`
}

// CollectMetadataHeader injects a trace ID into the request context.
// It reads X-Trace-ID from the incoming header; if absent, a new UUID is generated.
// The key stored matches logger.XTraceId so the logger can read it automatically.
func CollectMetadataHeader() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		traceID := c.GetHeader(string(logger.XTraceId))
		if traceID == "" {
			traceID = uuid.NewString()
		}

		idempotencyVal := c.GetHeader(string(IdempotencyKey))
		if idempotencyVal != "" {
			ctx = context.WithValue(ctx, IdempotencyKey, idempotencyVal)
		}

		ctx = context.WithValue(ctx, logger.XTraceId, traceID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

// LogMiddleware logs a structured request log entry after the handler returns.
func LogMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		ctx := c.Request.Context()
		logger.SetCustomField(ctx, logrus.Fields{
			"request_log": requestLog{
				Timestamp:    start,
				Method:       c.Request.Method,
				URL:          c.Request.URL.RequestURI(),
				Status:       c.Writer.Status(),
				ResponseTime: float64(time.Since(start).Milliseconds()),
				ResponseSize: c.Writer.Size(),
				ReqBody:      ctx.Value(ReqBodyKey),
			},
		})
		logger.Info(ctx)
	}
}

// WithLogReqBody reads the request body and stores it in the context so
// LogMiddleware can include it in the request log. The body is restored
// so downstream handlers can still read it.
func WithLogReqBody() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			bodyBytes, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			ctx := context.WithValue(c.Request.Context(), ReqBodyKey, string(bodyBytes))
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}
