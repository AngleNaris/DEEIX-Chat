package conversation

import (
	"context"
	"regexp"
	"strings"
	"time"

	appdynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/dynamicprompt"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/jseval"
	domaindynamicprompt "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/dynamicprompt"
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
	// scriptResolver 解析 {{script: name}} 动态提示词（nil 时脚本标签替换为空）。
	scriptResolver func(name string) string
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

// expandSystemPromptVars 展开提示词中的 {{var}} / {{js: ...}} / {{script: name}} 变量。
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
		if strings.HasPrefix(inner, "js:") || strings.HasPrefix(inner, "script:") {
			// js 与 script 标签共享脚本类限额，防 DoS。
			if jsCount >= maxJSVarsPerPrompt {
				return ""
			}
			jsCount++
			if strings.HasPrefix(inner, "js:") {
				code := strings.TrimSpace(strings.TrimPrefix(inner, "js:"))
				if code == "" || len(code) > maxJSVarCodeLen {
					return ""
				}
				return runPromptJSVar(code)
			}
			if vars.scriptResolver != nil {
				name := strings.TrimSpace(strings.TrimPrefix(inner, "script:"))
				if name != "" {
					return vars.scriptResolver(name)
				}
			}
			return ""
		}
		// 未知变量原样保留。
		return match
	})
}

// resolveSystemPromptVars 构建模板变量上下文；用户档案读取失败时
// 语言/用户名为空（时间类变量始终可用）。
//
// 时区优先级：usersettings.timezone（前端用浏览器时区写入）→ user.Timezone →
// 进程本地时区（容器默认 UTC）。时间变量按用户时区格式化，避免容器 UTC 偏差。
func (s *Service) resolveSystemPromptVars(ctx context.Context, userID uint) systemPromptVars {
	now := time.Now()
	loc, ok := s.resolveUserTimeZone(ctx, userID)
	if ok {
		now = now.In(loc)
	}
	vars := newSystemPromptVars(now, "", "")
	if userID == 0 || s.userProfile == nil {
		return vars
	}
	locale, username, _, err := s.userProfile.GetUserProfile(ctx, userID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("system_prompt_user_profile_failed", zap.Error(err))
		}
		return vars
	}
	vars.Language = locale
	vars.Username = username
	if s.dynamicPrompts != nil {
		scripts := s.getCachedDynamicPrompts(ctx, userID)
		if len(scripts) > 0 {
			vars.scriptResolver = func(name string) string {
				for _, prompt := range scripts {
					if prompt.Name == name && prompt.Enabled {
						return expandDynamicPrompt(prompt)
					}
				}
				return ""
			}
		}
	}
	return vars
}

// dynamicPromptCacheTTL 动态提示词列表缓存时长（写入后即时失效）。
const dynamicPromptCacheTTL = 3 * time.Minute

type cachedDynamicPrompts struct {
	prompts   []appdynamicprompt.PromptView
	expiresAt time.Time
}

// getCachedDynamicPrompts 读取用户动态提示词（带缓存回填）。
func (s *Service) getCachedDynamicPrompts(ctx context.Context, userID uint) []appdynamicprompt.PromptView {
	if userID == 0 || s.dynamicPrompts == nil {
		return nil
	}
	if cached, ok := s.dynamicPromptCache.Load(userID); ok {
		entry, ok2 := cached.(*cachedDynamicPrompts)
		if ok2 && time.Now().Before(entry.expiresAt) {
			return entry.prompts
		}
	}
	prompts, err := s.dynamicPrompts.ListDynamicPrompts(ctx, userID)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("dynamic_prompts_list_failed", zap.Error(err))
		}
		return nil
	}
	s.dynamicPromptCache.Store(userID, &cachedDynamicPrompts{
		prompts:   prompts,
		expiresAt: time.Now().Add(dynamicPromptCacheTTL),
	})
	return prompts
}

// expandDynamicPrompt 展开单个动态提示词：js 沙箱执行；text 直接插入（截断）。
func expandDynamicPrompt(prompt appdynamicprompt.PromptView) string {
	content := strings.TrimSpace(prompt.Content)
	if content == "" {
		return ""
	}
	if prompt.Kind == domaindynamicprompt.KindJS {
		return runPromptJSVar(content)
	}
	// text：与 js 输出同限制，防止注入膨胀。
	if runes := []rune(content); len(runes) > jsVarMaxOutput {
		content = string(runes[:jsVarMaxOutput]) + "…"
	}
	return content
}

// resolveUserTimeZone 解析用户时区（IANA 名称）；非法/缺失回退 false（进程时区）。
func (s *Service) resolveUserTimeZone(ctx context.Context, userID uint) (*time.Location, bool) {
	if userID == 0 {
		return nil, false
	}
	if s.userSettingsSvc != nil {
		if settings, err := s.userSettingsSvc.ListSettings(ctx, userID); err == nil {
			if tz := strings.TrimSpace(settings["timezone"]); tz != "" && tz != "Etc/UTC" {
				if loc, err := time.LoadLocation(tz); err == nil {
					return loc, true
				}
			}
		}
	}
	if s.userProfile != nil {
		if _, _, timezone, err := s.userProfile.GetUserProfile(ctx, userID); err == nil {
			tz := strings.TrimSpace(timezone)
			if tz != "" && tz != "Etc/UTC" {
				if loc, err := time.LoadLocation(tz); err == nil {
					return loc, true
				}
			}
		}
	}
	return nil, false
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
