package credentials

import "github.com/gin-gonic/gin"

// Module 用户凭据域模块。
type Module struct {
	Handler *Handler
}

// NewModule 创建模块。
func NewModule(handler *Handler) *Module {
	return &Module{Handler: handler}
}

// RegisterRoutes 注册凭据域认证路由。
func (m *Module) RegisterRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/credentials", m.Handler.ListCredentials)
	authRequired.POST("/credentials", m.Handler.CreateCredential)
	authRequired.PUT("/credentials/:id", m.Handler.UpdateCredential)
	authRequired.DELETE("/credentials/:id", m.Handler.DeleteCredential)
}
