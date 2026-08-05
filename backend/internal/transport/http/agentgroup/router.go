package agentgroup

import "github.com/gin-gonic/gin"

// RegisterRoutes 注册群组域路由。
func (m *Module) RegisterRoutes(authRequired *gin.RouterGroup) {
	authRequired.GET("/conversation-agent-groups", m.Handler.ListAgentGroups)
	authRequired.POST("/conversation-agent-groups", m.Handler.CreateAgentGroup)
	authRequired.GET("/conversation-agent-groups/feature", m.Handler.GetAgentGroupFeature)
	authRequired.GET("/conversation-agent-groups/:id", m.Handler.GetAgentGroup)
	authRequired.PATCH("/conversation-agent-groups/:id", m.Handler.UpdateAgentGroup)
	authRequired.DELETE("/conversation-agent-groups/:id", m.Handler.DeleteAgentGroup)
	authRequired.POST("/conversation-agent-groups/:id/members", m.Handler.AddAgentGroupMember)
	authRequired.POST("/conversation-agent-groups/:id/members/reorder", m.Handler.ReorderAgentGroupMembers)
	authRequired.PATCH("/conversation-agent-groups/:id/members/:member_id", m.Handler.UpdateAgentGroupMember)
	authRequired.DELETE("/conversation-agent-groups/:id/members/:member_id", m.Handler.RemoveAgentGroupMember)
	authRequired.POST("/conversation-agent-groups/:id/supervisor", m.Handler.ChangeAgentGroupSupervisor)
	authRequired.GET("/conversation-agent-group-runs/lookup", m.Handler.LookupAgentGroupRunDetailByClientRunID)
	authRequired.GET("/conversation-agent-group-runs/:run_id", m.Handler.GetAgentGroupRunDetail)
	authRequired.POST("/agent-group-runs/:run_id/steps/:step_id/retry", m.Handler.RetryAgentGroupRunStep)
	authRequired.POST("/agent-group-runs/:run_id/cancel", m.Handler.CancelAgentGroupRun)
	authRequired.POST("/agent-group-runs/:run_id/abandon", m.Handler.AbandonAgentGroupRun)
}
