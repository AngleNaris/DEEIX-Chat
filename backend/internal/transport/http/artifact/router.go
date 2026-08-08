package artifact

import "github.com/gin-gonic/gin"

// Module 制品域模块。
type Module struct {
	Handler *Handler
}

// NewModule 创建模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}

// RegisterRoutes 注册制品域认证路由。
func (m *Module) RegisterRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/artifacts", m.Handler.ListArtifacts)
	authRequired.POST("/artifacts", m.Handler.CreateArtifact)
	authRequired.GET("/artifacts/:id", m.Handler.GetArtifact)
	authRequired.DELETE("/artifacts/:id", m.Handler.DeleteArtifact)
	authRequired.POST("/artifacts/:id/share", m.Handler.CreateShare)
	authRequired.GET("/artifacts/:id/share", m.Handler.GetShare)
	authRequired.DELETE("/artifacts/:id/share", m.Handler.RevokeShare)
}

// RegisterPublicRoutes 注册制品域公开路由。
func (m *Module) RegisterPublicRoutes(public *gin.RouterGroup) {
	public.GET("/shared-artifacts/:share_id", m.Handler.GetPublicShare)
}
