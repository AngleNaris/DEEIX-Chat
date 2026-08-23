package skill

import "github.com/gin-gonic/gin"

// RegisterRoutes 注册技能用户侧路由。
func (m *Module) RegisterRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/skills", m.Handler.ListVisibleSkills)
	authRequired.GET("/skills/mine", m.Handler.ListMySkills)
	authRequired.POST("/skills/mine", m.Handler.CreateMySkill)
	authRequired.POST("/skills/mine/import/preview", m.Handler.PreviewMySkillPackage)
	authRequired.POST("/skills/mine/import", m.Handler.ImportMySkillPackage)
	authRequired.POST("/skills/mine/:id/package", m.Handler.ReplaceMySkillPackage)
	authRequired.PATCH("/skills/mine/:id", m.Handler.PatchMySkill)
	authRequired.DELETE("/skills/mine/:id", m.Handler.DeleteMySkill)
	authRequired.GET("/skills/:id", m.Handler.GetVisibleSkill)
	authRequired.GET("/skills/:id/package-file", m.Handler.GetSkillPackageFile)
}

// RegisterAdminRoutes 注册技能管理路由。
func (m *Module) RegisterAdminRoutes(adminGroup *gin.RouterGroup) {
	adminGroup.GET("/skills", m.Handler.ListAdminSkills)
	adminGroup.POST("/skills", m.Handler.CreateAdminSkill)
	adminGroup.POST("/skills/import/preview", m.Handler.PreviewAdminSkillPackage)
	adminGroup.POST("/skills/import", m.Handler.ImportAdminSkillPackage)
	adminGroup.POST("/skills/:id/package", m.Handler.ReplaceAdminSkillPackage)
	adminGroup.PATCH("/skills/:id", m.Handler.PatchAdminSkill)
	adminGroup.DELETE("/skills/:id", m.Handler.DeleteAdminSkill)
}
