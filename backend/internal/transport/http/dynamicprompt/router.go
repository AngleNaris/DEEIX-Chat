package dynamicprompt

import "github.com/gin-gonic/gin"

// Module 动态提示词域模块。
type Module struct {
	Handler *Handler
}

// NewModule 创建模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}

// RegisterRoutes 注册动态提示词域认证路由。
func (m *Module) RegisterRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/dynamic-prompts", m.Handler.ListPrompts)
	authRequired.POST("/dynamic-prompts", m.Handler.CreatePrompt)
	authRequired.PUT("/dynamic-prompts/:id", m.Handler.UpdatePrompt)
	authRequired.DELETE("/dynamic-prompts/:id", m.Handler.DeletePrompt)
}
