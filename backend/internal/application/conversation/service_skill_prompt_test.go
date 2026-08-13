package conversation

import (
	"context"
	"fmt"
	"strings"
	"testing"

	appskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/application/skill"
	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

func TestRenderSkillPromptIncludesSelectedSkillContent(t *testing.T) {
	prompt := &skillPrompts{
		Skills: []domainskill.Skill{
			{
				ID:          12,
				Scope:       domainskill.ScopeUser,
				Title:       "Review",
				Trigger:     "review",
				Description: "Review code",
				Markdown:    "Review the diff and return prioritized findings.",
			},
			{
				ID:       13,
				Scope:    domainskill.ScopeBuiltin,
				Title:    "Frontend Rules",
				Trigger:  "frontend",
				Markdown: "Check layout, spacing, and interaction states.",
			},
		},
	}
	rendered := renderSkillPrompts(prompt, "")
	for _, want := range []string{
		"<skill_context>",
		"<skills count=\"2\">",
		"<title>Review</title>",
		"<title>Frontend Rules</title>",
		"<description>Review code</description>",
		"<content>Review the diff and return prioritized findings.</content>",
		"<content>Check layout, spacing, and interaction states.</content>",
		"Each selected skill includes title, trigger, description, and SKILL.md content",
		"Use each skill's content when it is relevant",
		"Do not invent hidden instructions",
		"do not grant permission to execute operating-system commands",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered prompt to contain %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "Apply this skill") {
		t.Fatalf("expected rendered prompt not to force skill application:\n%s", rendered)
	}
}

func TestInjectSkillPromptAddsSystemMessageAfterExistingPolicy(t *testing.T) {
	prompt := &skillPrompts{
		Skills: []domainskill.Skill{{
			ID:       7,
			Scope:    domainskill.ScopeBuiltin,
			Title:    "Plan",
			Trigger:  "plan",
			Markdown: "Create a concise plan.",
		}},
	}
	prompt.Rendered = renderSkillPrompts(prompt, "")

	messages := injectSkillPrompts([]llm.Message{
		{Role: "system", Content: "base policy"},
		{Role: "user", Content: "hello"},
	}, prompt)

	if len(messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(messages))
	}
	if messages[0].Content != "base policy" || messages[1].Role != "system" || !strings.Contains(messages[1].Content, skillPromptSystemMarker) {
		t.Fatalf("expected skill prompt after base system policy: %#v", messages)
	}
	if messages[2].Role != "user" || messages[2].Content != "hello" {
		t.Fatalf("expected original user message last: %#v", messages)
	}
}

func TestRenderSkillPromptUsesCustomContract(t *testing.T) {
	prompt := &skillPrompts{
		Skills: []domainskill.Skill{{
			ID:       7,
			Scope:    domainskill.ScopeBuiltin,
			Title:    "Plan",
			Trigger:  "plan",
			Markdown: "Create a concise plan.",
		}},
	}

	rendered := renderSkillPrompts(prompt, "Use selected skills only when they directly match the user request.")
	for _, want := range []string{
		"<skills count=\"1\">",
		"<content>Create a concise plan.</content>",
		"Use selected skills only when they directly match the user request.",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered prompt to contain %q:\n%s", want, rendered)
		}
	}
	if strings.Contains(rendered, "These user-selected skills are available") {
		t.Fatalf("expected custom contract to replace default contract:\n%s", rendered)
	}
}

func TestResolveSkillPromptsRejectsTooManySelectedSkills(t *testing.T) {
	skillIDs := make([]uint, maxSelectedSkillsPerMessage+1)
	for index := range skillIDs {
		skillIDs[index] = uint(index + 1)
	}
	service := &Service{}
	_, err := service.resolveSkillPrompts(context.Background(), SendMessageInput{
		SkillIDs: skillIDs,
	})
	if err != ErrTooManySelectedSkills {
		t.Fatalf("expected ErrTooManySelectedSkills, got %v", err)
	}
}

func TestNormalizeSelectedSkillIDsDeduplicatesAndDropsEmpty(t *testing.T) {
	got := normalizeSelectedSkillIDs([]uint{0, 2, 2, 3, 0, 1})
	want := []uint{2, 3, 1}
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestRenderSkillPromptIncludesPackageFileManifest(t *testing.T) {
	prompt := &skillPrompts{
		Skills: []domainskill.Skill{
			{
				ID:          12,
				Scope:       domainskill.ScopeUser,
				Title:       "Dice",
				Trigger:     "dice",
				Markdown:    "Roll dice by reading the script.",
				PackageType: domainskill.PackageTypePackage,
				PackageFiles: []domainskill.PackageFile{
					{Path: "scripts/roll.py", Size: 512, Kind: domainskill.FileKindText},
					{Path: "assets/icon.png", Size: 2048, Kind: domainskill.FileKindBinary},
				},
			},
			{
				ID:       13,
				Scope:    domainskill.ScopeBuiltin,
				Title:    "Plain",
				Trigger:  "plain",
				Markdown: "Text only skill.",
			},
		},
	}
	rendered := renderSkillPrompts(prompt, "")
	for _, want := range []string{
		"<files>",
		`<file path="scripts/roll.py" size="512" kind="text"/>`,
		`<file path="assets/icon.png" size="2048" kind="binary"/>`,
		"</files>",
		"<read_file path=\"relative/path\">",
	} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("expected rendered prompt to contain %q:\n%s", want, rendered)
		}
	}
	if !strings.Contains(rendered, "<title>Plain</title>") {
		t.Fatalf("expected text-only skill to be rendered, got:\n%s", rendered)
	}
	// 文本技能不含 <files> 块：整个渲染结果只应有包技能一个 files 块。
	// （用闭合标签 </files> 计数：contract 模板文案中的 "<files> block" 字样不含闭合标签）
	if strings.Count(rendered, "</files>") != 1 {
		t.Fatalf("expected exactly one <files> block for the package skill, got:\n%s", rendered)
	}
}

func TestReadFileTagScannerFiltersTagsAndCollectsRequests(t *testing.T) {
	scanner := newReadFileTagScanner()
	// 标记跨多个分片出现；过滤后仅输出普通文本。
	visible, requests := scanner.consume("请看脚本")
	if visible != "请看脚本" || len(requests) != 0 {
		t.Fatalf("unexpected first consume: visible=%q requests=%v", visible, requests)
	}
	visible, requests = scanner.consume("：<read_file path=\"sc")
	if visible != "：" || len(requests) != 0 {
		t.Fatalf("unexpected partial consume: visible=%q requests=%v", visible, requests)
	}
	visible, requests = scanner.consume("ripts/roll.py\">")
	if visible != "" || len(requests) != 1 || requests[0].Path != "scripts/roll.py" {
		t.Fatalf("unexpected tag consume: visible=%q requests=%v", visible, requests)
	}
	visible, requests = scanner.consume("。这是结果。")
	if visible != "。这是结果。" || len(requests) != 0 {
		t.Fatalf("unexpected tail consume: visible=%q requests=%v", visible, requests)
	}
	if len(scanner.requests) != 0 {
		t.Fatalf("expected scanner requests drained, got %v", scanner.requests)
	}
}

func TestReadFileTagScannerLeavesMalformedTagsVisible(t *testing.T) {
	scanner := newReadFileTagScanner()
	// 缺 path 属性的标记按普通文本输出。
	visible, requests := scanner.consume("<read_file>")
	if visible != "<read_file>" || len(requests) != 0 {
		t.Fatalf("unexpected malformed tag consume: visible=%q requests=%v", visible, requests)
	}
	// 前缀与 <read_file 不一致的内容立即输出。
	visible, requests = scanner.consume("<read_filex>text")
	if visible != "<read_filex>text" || len(requests) != 0 {
		t.Fatalf("unexpected prefix mismatch consume: visible=%q requests=%v", visible, requests)
	}
	// 自闭合形式。
	visible, requests = scanner.consume(`<read_file path="a.txt"/>`)
	if visible != "" || len(requests) != 1 || requests[0].Path != "a.txt" {
		t.Fatalf("unexpected self-closing consume: visible=%q requests=%v", visible, requests)
	}
}

func TestParseReadFileTagRejectsInvalidForms(t *testing.T) {
	for _, tag := range []string{
		`<read_file>`,
		`<read_file path>`,
		`<read_file path=>`,
		`<read_file path=""/>`,
		`<read_file src="x">`,
		`<read_file path='x'>`,
		`<read_file path="a"`,
	} {
		if _, ok := parseReadFileTag([]byte(tag)); ok {
			t.Fatalf("expected %q to be rejected", tag)
		}
	}
	for _, tag := range []string{
		`<read_file path="a.txt">`,
		`<read_file path="a.txt"/>`,
		`<read_file path="dir/a.txt" >`,
	} {
		req, ok := parseReadFileTag([]byte(tag))
		if !ok || req.Path == "" {
			t.Fatalf("expected %q to be accepted", tag)
		}
	}
}

type fakeSkillFileResolver struct {
	files map[uint]map[string]string
}

func (f *fakeSkillFileResolver) ResolveAvailable(ctx context.Context, userID uint, id uint) (*domainskill.Skill, error) {
	return nil, nil
}

func (f *fakeSkillFileResolver) ListVisible(ctx context.Context, userID uint, input appskill.ListInput) ([]domainskill.Skill, int64, error) {
	return nil, 0, nil
}

func (f *fakeSkillFileResolver) GetPackageFile(ctx context.Context, userID uint, skillID uint, filePath string) ([]byte, error) {
	content, ok := f.files[skillID][filePath]
	if !ok {
		return nil, appskill.ErrPackageFileNotFound
	}
	return []byte(content), nil
}

func (f *fakeSkillFileResolver) UpdateUser(ctx context.Context, userID uint, id uint, input appskill.PatchInput) (*domainskill.Skill, error) {
	return nil, nil
}

func (f *fakeSkillFileResolver) CreateUser(ctx context.Context, userID uint, input appskill.WriteInput) (*domainskill.Skill, error) {
	return nil, nil
}

func (f *fakeSkillFileResolver) DeleteUser(ctx context.Context, userID uint, id uint) error {
	return nil
}

func TestResolveSkillFileRequestsDisclosesOnlyManifestFiles(t *testing.T) {
	resolver := &fakeSkillFileResolver{files: map[uint]map[string]string{
		1: {"scripts/roll.py": "import random\nprint(random.randint(1, 6))\n", "secret.txt": "top secret"},
	}}
	service := &Service{skillResolver: resolver}
	prompt := &skillPrompts{Skills: []domainskill.Skill{{
		ID:          1,
		PackageType: domainskill.PackageTypePackage,
		PackageFiles: []domainskill.PackageFile{
			{Path: "scripts/roll.py", Size: 45, Kind: domainskill.FileKindText},
		},
	}}}
	messages, loaded := service.resolveSkillFileRequests(context.Background(), 42, prompt, []skillFileRequest{
		{Path: "scripts/roll.py"},
		{Path: "secret.txt"},    // 不在清单中：忽略
		{Path: "../etc/passwd"}, // 越界：忽略
	})
	if len(loaded) != 1 || loaded[0] != "scripts/roll.py" {
		t.Fatalf("expected only manifest file loaded, got %v", loaded)
	}
	if len(messages) != 1 || messages[0].Role != "system" || !strings.Contains(messages[0].Content, "random.randint") {
		t.Fatalf("unexpected messages: %#v", messages)
	}
	if !strings.Contains(messages[0].Content, "skill_id=\"1\"") || !strings.Contains(messages[0].Content, "scripts/roll.py") {
		t.Fatalf("expected file content wrapper with skill id and path: %s", messages[0].Content)
	}
}

func TestResolveSkillFileRequestsRespectsBudgetAndLimit(t *testing.T) {
	big := strings.Repeat("x", 40<<10)
	files := make(map[string]string)
	manifest := make([]domainskill.PackageFile, 0, 10)
	for i := 0; i < 9; i++ {
		path := fmt.Sprintf("big/%d.txt", i)
		files[path] = big
		manifest = append(manifest, domainskill.PackageFile{Path: path, Size: int64(len(big)), Kind: domainskill.FileKindText})
	}
	resolver := &fakeSkillFileResolver{files: map[uint]map[string]string{1: files}}
	service := &Service{skillResolver: resolver}
	prompt := &skillPrompts{Skills: []domainskill.Skill{{
		ID:           1,
		PackageType:  domainskill.PackageTypePackage,
		PackageFiles: manifest,
	}}}
	requests := make([]skillFileRequest, 0, 9)
	for i := 0; i < 9; i++ {
		requests = append(requests, skillFileRequest{Path: fmt.Sprintf("big/%d.txt", i)})
	}
	messages, loaded := service.resolveSkillFileRequests(context.Background(), 42, prompt, requests)
	// 请求数上限 8，但每个文件 40KB 截断到 32KB 后，64KB 总预算只够前 2 个文件。
	if len(loaded) != 2 {
		t.Fatalf("expected 2 files within budget, got %d: %v", len(loaded), loaded)
	}
	totalBytes := 0
	for _, message := range messages {
		totalBytes += len(message.Content)
	}
	// 每个文件截断到 32KB，总量不超过 64KB 预算（8 个请求按顺序，前两个 32KB 后预算耗尽前最多 64KB）。
	if totalBytes > skillFileReadBudgetBytes+skillFileReadLimitBytes {
		t.Fatalf("expected total bytes within budget, got %d", totalBytes)
	}
	if len(messages) != len(loaded) {
		t.Fatalf("expected message count to match loaded count: %d vs %d", len(messages), len(loaded))
	}
}
