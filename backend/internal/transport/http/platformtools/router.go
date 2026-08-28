package platformtools

import (
	"github.com/gin-gonic/gin"
)

// Module 聚合平台工具 HTTP 处理器。
type Module struct {
	Handler *Handler
}

// NewModule 创建平台工具 HTTP 模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}

// RegisterRoutes 注册平台工具路由（需要登录）。
func (m *Module) RegisterRoutes(authGroup *gin.RouterGroup) {
	g := authGroup.Group("/platform-tools/approvals")
	g.GET("/:approval_id", m.Handler.GetApproval)
	g.POST("/:approval_id/approve", m.Handler.Approve)
	g.POST("/:approval_id/reject", m.Handler.Reject)
}
