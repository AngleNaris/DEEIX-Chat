package agentgroup

// AuditInput 描述群组域一次审计写入（§18 分享、导出与审计）。
type AuditInput struct {
	UserID     uint
	RequestID  string
	Action     string
	Resource   string
	ResourceID string
	ClientIP   string
	UserAgent  string
	Detail     interface{}
}
