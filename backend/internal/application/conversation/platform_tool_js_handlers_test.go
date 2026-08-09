package conversation

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	appskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
)

// fakeSkillResolver 模拟 skillResolver 接口（仅 JS 执行测试需要 GetPackageFile）。
type fakeSkillResolver struct {
	files map[string][]byte // path → content
	err   error
}

func (f *fakeSkillResolver) ResolveAvailable(ctx context.Context, userID uint, id uint) (*domainskill.Skill, error) {
	return &domainskill.Skill{ID: id}, nil
}

func (f *fakeSkillResolver) ListVisible(ctx context.Context, userID uint, input appskill.ListInput) ([]domainskill.Skill, int64, error) {
	return nil, 0, nil
}

func (f *fakeSkillResolver) GetPackageFile(ctx context.Context, userID uint, skillID uint, filePath string) ([]byte, error) {
	if f.err != nil {
		return nil, f.err
	}
	content, ok := f.files[filePath]
	if !ok {
		return nil, errors.New("file not found")
	}
	return content, nil
}

func (f *fakeSkillResolver) UpdateUser(ctx context.Context, userID uint, id uint, input appskill.PatchInput) (*domainskill.Skill, error) {
	return nil, nil
}

func (f *fakeSkillResolver) CreateUser(ctx context.Context, userID uint, input appskill.WriteInput) (*domainskill.Skill, error) {
	return nil, nil
}

func (f *fakeSkillResolver) DeleteUser(ctx context.Context, userID uint, id uint) error {
	return nil
}

func TestPlatformExecuteJs(t *testing.T) {
	svc := &Service{}
	out, err := svc.platformExecuteJs(context.Background(), platformToolCallContext{
		UserID:    1,
		RequestID: "req-js",
		Arguments: json.RawMessage(`{"code":"const n = Math.floor(Math.random() * 100); console.log('random:', n); n;"}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Status string `json:"status"`
		Stdout string `json:"stdout"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad output %q: %v", out, err)
	}
	if result.Status != "ok" {
		t.Fatalf("expected ok status, got %q", result.Status)
	}
	if !strings.Contains(result.Stdout, "random:") {
		t.Fatalf("stdout missing random output: %q", result.Stdout)
	}
	if result.Result == "" {
		t.Fatalf("expected numeric result")
	}
}

func TestPlatformExecuteJsValidation(t *testing.T) {
	svc := &Service{}
	// code 必填。
	if _, err := svc.platformExecuteJs(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{}`),
	}); err == nil || !strings.Contains(err.Error(), "code is required") {
		t.Fatalf("expected code required error, got %v", err)
	}
	// code 超长。
	longCode := `"` + strings.Repeat("x", platformJSCodeLimitBytes+1) + `"`
	if _, err := svc.platformExecuteJs(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"code":` + longCode + `}`),
	}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("expected length error, got %v", err)
	}
	// 脚本错误作为正常工具结果返回（status=error）。
	out, err := svc.platformExecuteJs(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"code":"throw new Error('boom')"}`),
	})
	if err != nil {
		t.Fatalf("script errors must be tool results, got %v", err)
	}
	var result struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad output: %v", err)
	}
	if result.Status != "error" || !strings.Contains(result.Error, "boom") {
		t.Fatalf("unexpected error result: %+v", result)
	}
}

func TestPlatformExecuteSkillScript(t *testing.T) {
	svc := &Service{skillResolver: &fakeSkillResolver{
		files: map[string][]byte{
			"scripts/tool.js": []byte(`console.log('sum:', args[0] + args[1]); args[0] + args[1];`),
		},
	}}
	out, err := svc.platformExecuteSkillScript(context.Background(), platformToolCallContext{
		UserID:    1,
		RequestID: "req-script",
		Arguments: json.RawMessage(`{"skill_id":7,"path":"scripts/tool.js","args":[2,3]}`),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result struct {
		Status   string `json:"status"`
		Stdout   string `json:"stdout"`
		Result   string `json:"result"`
		SkillID  uint   `json:"skill_id"`
		Path     string `json:"path"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("bad output %q: %v", out, err)
	}
	if result.Status != "ok" || !strings.Contains(result.Stdout, "sum: 5") || result.Result != "5" {
		t.Fatalf("unexpected execution result: %+v", result)
	}
	if result.SkillID != 7 || result.Path != "scripts/tool.js" {
		t.Fatalf("unexpected meta: %+v", result)
	}
}

func TestPlatformExecuteSkillScriptValidation(t *testing.T) {
	svc := &Service{skillResolver: &fakeSkillResolver{
		files: map[string][]byte{"scripts/tool.js": []byte(`1;`)},
	}}
	// skill_id 必填。
	if _, err := svc.platformExecuteSkillScript(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"path":"scripts/tool.js"}`),
	}); err == nil || !strings.Contains(err.Error(), "skill_id") {
		t.Fatalf("expected skill_id required, got %v", err)
	}
	// path 必填。
	if _, err := svc.platformExecuteSkillScript(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"skill_id":7}`),
	}); err == nil || !strings.Contains(err.Error(), "path is required") {
		t.Fatalf("expected path required, got %v", err)
	}
	// 非 js 扩展名拒绝。
	if _, err := svc.platformExecuteSkillScript(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"skill_id":7,"path":"scripts/tool.py"}`),
	}); err == nil || !strings.Contains(err.Error(), "javascript file") {
		t.Fatalf("expected extension rejection, got %v", err)
	}
	// GetPackageFile 错误传播。
	svcErr := &Service{skillResolver: &fakeSkillResolver{err: errors.New("not in manifest")}}
	if _, err := svcErr.platformExecuteSkillScript(context.Background(), platformToolCallContext{
		UserID:    1,
		Arguments: json.RawMessage(`{"skill_id":7,"path":"scripts/tool.js"}`),
	}); err == nil || !strings.Contains(err.Error(), "not in manifest") {
		t.Fatalf("expected resolver error propagation, got %v", err)
	}
}
