package platformtools

// ApprovalResponse 批准/拒绝平台工具写操作的响应。
type ApprovalResponse struct {
	// Approval 待批准记录摘要（JSON 字符串，含 approval_id/tool/arguments/status）。
	Approval string `json:"approval"`
}
