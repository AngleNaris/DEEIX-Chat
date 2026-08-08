package doccard

import "github.com/gin-gonic/gin"

// Module 文档卡片域模块。
type Module struct {
	Handler *Handler
}

// NewModule 创建模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}

// RegisterRoutes 注册文档卡片域认证路由。
func (m *Module) RegisterRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/doc-cards", m.Handler.ListDocCards)
	authRequired.POST("/doc-cards", m.Handler.CreateDocCard)
	authRequired.PUT("/doc-cards/:id", m.Handler.UpdateDocCard)
	authRequired.DELETE("/doc-cards/:id", m.Handler.DeleteDocCard)
}
