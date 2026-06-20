package v1

import (
	"context"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/dhuki/go-ledger-system/internal/adapter/http/middleware"
)

type Handler interface {
	RegisterRoute(g *gin.RouterGroup)
}

type svc struct {
	engine  *gin.Engine
	server  *http.Server
	handler []Handler
}

func NewHTTPRouter(port int, handlers ...Handler) *svc {
	gin.SetMode(gin.ReleaseMode)
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(middleware.CollectMetadataHeader())
	engine.Use(middleware.LogMiddleware())

	return &svc{
		engine: engine,
		server: &http.Server{
			Addr:    fmt.Sprintf(":%d", port),
			Handler: engine,
		},
		handler: handlers,
	}
}

func (s *svc) Name() string {
	return "HTTP Client"
}

func (s *svc) RegisterHandler() {
	v1Group := s.engine.Group("/api/v1")
	for _, h := range s.handler {
		h.RegisterRoute(v1Group)
	}
}

func (s *svc) Start(ctx context.Context) error {
	s.RegisterHandler()
	return s.server.ListenAndServe()
}

func (s *svc) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
