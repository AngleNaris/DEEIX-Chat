package skill

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
)

func buildZip(t *testing.T, files map[string]string, symlinkNames ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	for name, content := range files {
		file, err := writer.Create(name)
		if err != nil {
			t.Fatalf("create zip entry %q: %v", name, err)
		}
		if _, err := file.Write([]byte(content)); err != nil {
			t.Fatalf("write zip entry %q: %v", name, err)
		}
	}
	for _, name := range symlinkNames {
		header := &zip.FileHeader{Name: name}
		header.SetMode(0o777 | os.ModeSymlink) // symlink mode
		file, err := writer.CreateHeader(header)
		if err != nil {
			t.Fatalf("create symlink entry %q: %v", name, err)
		}
		if _, err := file.Write([]byte("target")); err != nil {
			t.Fatalf("write symlink entry %q: %v", name, err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	return buf.Bytes()
}

func TestParsePackageRootSkillMarkdownWithFrontmatter(t *testing.T) {
	data := buildZip(t, map[string]string{
		"SKILL.md": "---\nname: Dice Roller\ndescription: Rolls dice with a script\n---\n\nRoll the dice using the helper script.\n",
		"scripts/roll.py": "import random\nprint(random.randint(1, 6))\n",
		"assets/icon.png":  "\x89PNG\r\n\x1a\nbinary-content",
	})
	preview, contents, err := ParsePackage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.Title != "Dice Roller" || preview.Trigger != "Dice Roller" {
		t.Fatalf("unexpected title/trigger: %q/%q", preview.Title, preview.Trigger)
	}
	if preview.Description != "Rolls dice with a script" {
		t.Fatalf("unexpected description: %q", preview.Description)
	}
	if !strings.Contains(preview.Markdown, "Roll the dice") || strings.Contains(preview.Markdown, "name:") {
		t.Fatalf("expected frontmatter stripped from markdown:\n%s", preview.Markdown)
	}
	if preview.RootDir != "" {
		t.Fatalf("expected empty root dir, got %q", preview.RootDir)
	}
	if len(preview.Files) != 2 {
		t.Fatalf("expected 2 files, got %d: %v", len(preview.Files), preview.Files)
	}
	byPath := make(map[string]domainskill.PackageFile, len(preview.Files))
	for _, file := range preview.Files {
		byPath[file.Path] = file
	}
	if file, ok := byPath["scripts/roll.py"]; !ok || file.Kind != domainskill.FileKindText {
		t.Fatalf("expected text kind for roll.py, got %v", byPath)
	}
	if file, ok := byPath["assets/icon.png"]; !ok || file.Kind != domainskill.FileKindBinary {
		t.Fatalf("expected binary kind for icon.png, got %v", byPath)
	}
	if len(contents) != 2 || !bytes.Contains(contents["scripts/roll.py"], []byte("random.randint")) {
		t.Fatalf("unexpected contents map: %v", contents)
	}
}

func TestParsePackageStripsSingleTopLevelDirectory(t *testing.T) {
	data := buildZip(t, map[string]string{
		"dice-skill/SKILL.md":       "Roll dice.",
		"dice-skill/scripts/roll.py": "print('dice')",
	})
	preview, contents, err := ParsePackage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if preview.RootDir != "dice-skill" {
		t.Fatalf("expected root dir dice-skill, got %q", preview.RootDir)
	}
	if preview.Title != "Dice Skill" {
		t.Fatalf("expected title derived from root dir, got %q", preview.Title)
	}
	if preview.Trigger != "dice-skill" {
		t.Fatalf("expected trigger derived from root dir, got %q", preview.Trigger)
	}
	if len(preview.Files) != 1 || preview.Files[0].Path != "scripts/roll.py" {
		t.Fatalf("expected path relative to root dir, got %v", preview.Files)
	}
	if _, ok := contents["scripts/roll.py"]; !ok {
		t.Fatalf("expected contents keyed by relative path")
	}
}

func TestParsePackageRejectsZipSlipAndAbsolutePaths(t *testing.T) {
	for _, name := range []string{"../evil.txt", "../../etc/passwd", "/etc/passwd", "a/../../evil.txt"} {
		data := buildZip(t, map[string]string{
			"SKILL.md": "test",
			name:       "evil",
		})
		if _, _, err := ParsePackage(data); err != ErrInvalidPackage {
			t.Fatalf("expected ErrInvalidPackage for %q, got %v", name, err)
		}
	}
}

func TestParsePackageRejectsSymlink(t *testing.T) {
	data := buildZip(t, map[string]string{"SKILL.md": "test"}, "link.txt")
	if _, _, err := ParsePackage(data); err != ErrInvalidPackage {
		t.Fatalf("expected ErrInvalidPackage for symlink, got %v", err)
	}
}

func TestParsePackageRejectsTooManyFiles(t *testing.T) {
	files := map[string]string{"SKILL.md": "test"}
	for i := 0; i < maxPackageFileCount+5; i++ {
		files[fmt.Sprintf("extra-%d.txt", i)] = "x"
	}
	data := buildZip(t, files)
	if _, _, err := ParsePackage(data); err != ErrInvalidPackage {
		t.Fatalf("expected ErrInvalidPackage for too many files, got %v", err)
	}
}

func TestParsePackageRejectsMissingSkillMarkdown(t *testing.T) {
	data := buildZip(t, map[string]string{"readme.txt": "no skill here"})
	if _, _, err := ParsePackage(data); err != ErrInvalidPackage {
		t.Fatalf("expected ErrInvalidPackage without SKILL.md, got %v", err)
	}
}

func TestParsePackageRejectsMultipleTopLevelDirectories(t *testing.T) {
	data := buildZip(t, map[string]string{
		"skill-a/SKILL.md": "one",
		"skill-b/readme.md": "two",
	})
	if _, _, err := ParsePackage(data); err != ErrInvalidPackage {
		t.Fatalf("expected ErrInvalidPackage for multiple top dirs, got %v", err)
	}
}

func TestStripFrontmatter(t *testing.T) {
	body, meta := stripFrontmatter("---\nname: Foo\ndescription: Bar\n---\nBody text\n")
	if body != "Body text" {
		t.Fatalf("unexpected body %q", body)
	}
	if meta["name"] != "Foo" || meta["description"] != "Bar" {
		t.Fatalf("unexpected meta %v", meta)
	}

	// 无 frontmatter：原文返回。
	body, meta = stripFrontmatter("plain text")
	if body != "plain text" || meta != nil {
		t.Fatalf("expected plain text unchanged, got %q %v", body, meta)
	}

	// 损坏 frontmatter：原文返回。
	body, meta = stripFrontmatter("---\nname: [unclosed\nbody")
	if body != "---\nname: [unclosed\nbody" {
		t.Fatalf("expected malformed frontmatter unchanged, got %q", body)
	}

	// BOM 前缀。
	body, meta = stripFrontmatter("\ufeff---\nname: BOM\n---\ncontent")
	if meta["name"] != "BOM" || body != "content" {
		t.Fatalf("unexpected BOM parse %q %v", body, meta)
	}
}

func TestSanitizeZipPath(t *testing.T) {
	for _, name := range []string{"../a", "a/../../b", "/abs"} {
		if _, ok := sanitizeZipPath(name); ok {
			t.Fatalf("expected %q rejected", name)
		}
	}
	for _, name := range []string{"a.txt", "dir/sub/a.txt", "a/./b.txt", "a/../b"} {
		clean, ok := sanitizeZipPath(name)
		if !ok || clean == "" {
			t.Fatalf("expected %q accepted, got %q", name, clean)
		}
	}
}

func TestIsLikelyTextSniffsUnknownExtension(t *testing.T) {
	data := buildZip(t, map[string]string{
		"SKILL.md":   "test",
		"data.custom": "hello text content",
	})
	preview, _, err := ParsePackage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(preview.Files) != 1 || preview.Files[0].Kind != domainskill.FileKindText {
		t.Fatalf("expected unknown extension sniffed as text, got %v", preview.Files)
	}
}
