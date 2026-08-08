package conversation

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/jseval"
	"go.uber.org/zap"
)

// 系统提示词模板变量：允许在用户可编辑的提示词（全局/模型/项目/角色）中
// 插入运行时信息与 JS 脚本输出，每次消息发送时展开取当前值。
//
// 支持：
//   - {{date}} / {{time}} / {{datetime}} / {{weekday}}：本地时区当前时间
//   - {{language}}：用户语言（Locale）；{{username}}：用户名
//   - {{js: 代码}}：纯计算沙箱执行（jseval），输出插入；失败替换为空串
//
// 未知变量原样保留，避免误伤提示词中的其他花括号内容。

const (
	// maxJSVarsPerPrompt 单个提示词中 js 变量的数量上限（防 DoS）。
	maxJSVarsPerPrompt = 3
	// maxJSVarCodeLen 单个 js 变量代码长度上限。
	maxJSVarCodeLen = 4096
	// jsVarTimeout 单个 js 变量执行超时。
	jsVarTimeout = time.Second
	// jsVarMaxOutput 单个 js 变量输出截断。
	jsVarMaxOutput = 4096
)

// 宽松非贪婪匹配：inner 语义（预定义变量名 / js: 代码 / 未知原样保留）在 Go 侧判断。
var systemPromptVarPattern = regexp.MustCompile(`\{\{(.*?)\}\}`)

// systemPromptVars 一次提示词渲染的变量上下文。
type systemPromptVars struct {
	Date     string
	Time     string
	DateTime string
	Weekday  string
	Language string
	Username string
}

// newSystemPromptVars 以当前时间为基准构建变量上下文。
func newSystemPromptVars(now time.Time, locale string, username string) systemPromptVars {
	return systemPromptVars{
		Date:     now.Format("2006-01-02"),
		Time:     now.Format("15:04:05"),
		DateTime: now.Format("2006-01-02 15:04:05"),
		Weekday:  now.Format("Monday"),
		Language: strings.TrimSpace(locale),
		Username: strings.TrimSpace(username),
	}
}

// expandSystemPromptVars 展开提示词中的 {{var}} 与 {{js: ...}} 变量。
func expandSystemPromptVars(text string, vars systemPromptVars) string {
	if !strings.Contains(text, "{{") {
		return text
	}
	jsCount := 0
	return systemPromptVarPattern.ReplaceAllStringFunc(text, func(match string) string {
		inner := strings.TrimSpace(match[2 : len(match)-2])
		switch inner {
		case "date":
			return vars.Date
		case "time":
			return vars.Time
		case "datetime":
			return vars.DateTime
		case "weekday":
			return vars.Weekday
		case "language":
			return vars.Language
		case "username":
			return vars.Username
		}
		if strings.HasPrefix(inner, "js:") {
			if jsCount >= maxJSVarsPerPrompt {
				return ""
			}
			jsCount++
			code := strings.TrimSpace(strings.TrimPrefix(inner, "js:"))
			if code == "" || len(code) > maxJSVarCodeLen {
				return ""
			}
			return runPromptJSVar(code)
		}
		// 未知变量原样保留。
		return match
	})
}

// resolveSystemPromptVars 构建模板变量上下文；用户档案读取失败时
// 语言/用户名为空（时间类变量始终可用）。
func (s *Service) resolveSystemPromptVars(ctx context.Context, userID uint) systemPromptVars {
	vars := newSystemPromptVars(time.Now(), "", "")
	if userID == 0 || s.userProfile == nil {
		return vars
	}
	locale, username, err := s.userProfile.GetUserProfile(ctx, userID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("system_prompt_user_profile_failed", zap.Error(err))
		}
		return vars
	}
	vars.Language = locale
	vars.Username = username
	return vars
}

// runPromptJSVar 在纯计算沙箱中执行 js 变量代码，优先返回完成值，
// 否则返回 console 输出；失败返回空串（不阻塞消息发送）。
func runPromptJSVar(code string) string {
	ctx, cancel := context.WithTimeout(context.Background(), jsVarTimeout)
	defer cancel()
	result, err := jseval.Run(ctx, code, jseval.Options{
		Timeout:        jsVarTimeout,
		MaxOutputBytes: jsVarMaxOutput,
	})
	if err != nil {
		return ""
	}
	if strings.TrimSpace(result.Result) != "" {
		return strings.TrimSpace(result.Result)
	}
	return strings.TrimSpace(result.Stdout)
}
