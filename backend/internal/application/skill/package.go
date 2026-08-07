package skill

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"unicode/utf8"

	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"gopkg.in/yaml.v3"
)

const (
	// maxPackageZipBytes 限制上传 zip 的大小。
	maxPackageZipBytes = 10 << 20 // 10MB
	// maxPackageTotalBytes 限制解压后文件总大小。
	maxPackageTotalBytes = 20 << 20 // 20MB
	// maxPackageFileCount 限制包内文件数量。
	maxPackageFileCount = 200
	// maxPackageFileReadBytes 限制单文件作为文本读取的字节上限。
	maxPackageFileReadBytes = 32 << 10 // 32KB
	// maxPackageInspectBytes 用于文本嗅探读取的字节数。
	maxPackageInspectBytes = 512

	skillMarkdownFileName = "SKILL.md"
)

// PackagePreview 描述解析后的技能包元数据，用于预览与导入。
type PackagePreview struct {
	Title       string
	Trigger     string
	Description string
	Markdown    string
	RootDir     string
	Files       []domainskill.PackageFile
}

// ParsePackage 从 zip 字节解析并校验标准技能包。
// 返回预览元数据与文件内容映射（相对路径 → 字节），内容供导入时写入对象存储。
func ParsePackage(data []byte) (*PackagePreview, map[string][]byte, error) {
	if len(data) == 0 {
		return nil, nil, ErrInvalidPackage
	}
	if len(data) > maxPackageZipBytes {
		return nil, nil, ErrInvalidPackage
	}
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, nil, ErrInvalidPackage
	}

	entries := make([]*zip.File, 0, len(reader.File))
	var totalBytes int64
	for _, file := range reader.File {
		name := strings.TrimSpace(file.Name)
		if name == "" || strings.HasSuffix(name, "/") {
			continue // 目录项
		}
		if file.Mode()&os.ModeSymlink != 0 {
			return nil, nil, ErrInvalidPackage
		}
		cleanName, ok := sanitizeZipPath(name)
		if !ok {
			return nil, nil, ErrInvalidPackage
		}
		if file.UncompressedSize64 > maxPackageTotalBytes || totalBytes+int64(file.UncompressedSize64) > maxPackageTotalBytes {
			return nil, nil, ErrInvalidPackage
		}
		if len(entries) >= maxPackageFileCount {
			return nil, nil, ErrInvalidPackage
		}
		totalBytes += int64(file.UncompressedSize64)
		file.Name = cleanName
		entries = append(entries, file)
	}
	if len(entries) == 0 {
		return nil, nil, ErrInvalidPackage
	}

	rootDir, skillEntry, err := locateSkillMarkdown(entries)
	if err != nil {
		return nil, nil, err
	}
	markdownRaw, err := readZipEntry(skillEntry, maxPackageTotalBytes)
	if err != nil {
		return nil, nil, ErrInvalidPackage
	}
	body, meta := stripFrontmatter(string(markdownRaw))
	if runeCount(body) > maxSkillMarkdownLength {
		return nil, nil, ErrInvalidPackage
	}

	preview := &PackagePreview{
		Title:    meta["name"],
		Trigger:  meta["name"],
		Markdown: body,
		RootDir:  rootDir,
	}
	if description := meta["description"]; description != "" {
		preview.Description = description
	}
	if preview.Title == "" {
		preview.Title = skillTitleFromRootDir(rootDir)
	}
	if preview.Trigger == "" {
		preview.Trigger = skillTriggerFromRootDir(rootDir)
	}
	if runeCount(preview.Title) > maxSkillTitleLength {
		return nil, nil, ErrInvalidPackage
	}
	if runeCount(preview.Trigger) > maxSkillTriggerLength {
		return nil, nil, ErrInvalidPackage
	}
	if runeCount(preview.Description) > maxSkillDescriptionLength {
		return nil, nil, ErrInvalidPackage
	}

	contents := make(map[string][]byte, len(entries))
	for _, file := range entries {
		if file == skillEntry {
			continue
		}
		rel := strings.TrimPrefix(path.Clean(file.Name), rootDir+"/")
		kind := domainskill.FileKindText
		if !isLikelyText(file) {
			kind = domainskill.FileKindBinary
		}
		preview.Files = append(preview.Files, domainskill.PackageFile{
			Path: rel,
			Size: int64(file.UncompressedSize64),
			Kind: kind,
		})
		raw, err := readZipEntry(file, maxPackageTotalBytes)
		if err != nil {
			return nil, nil, ErrInvalidPackage
		}
		contents[rel] = raw
	}
	sort.Slice(preview.Files, func(i, j int) bool {
		return preview.Files[i].Path < preview.Files[j].Path
	})
	return preview, contents, nil
}

// stripFrontmatter 解析 SKILL.md 开头的 YAML frontmatter（--- 包裹），
// 返回正文和 frontmatter 元数据。frontmatter 缺失或损坏时返回原文。
func stripFrontmatter(content string) (body string, meta map[string]string) {
	trimmed := strings.TrimPrefix(content, "\ufeff")
	lines := strings.Split(trimmed, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return content, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return content, nil
	}
	header := strings.Join(lines[1:end], "\n")
	var raw map[string]interface{}
	if err := yaml.Unmarshal([]byte(header), &raw); err != nil {
		return content, nil
	}
	meta = make(map[string]string, len(raw))
	for key, value := range raw {
		switch typed := value.(type) {
		case string:
			meta[strings.ToLower(key)] = typed
		case fmt.Stringer:
			meta[strings.ToLower(key)] = typed.String()
		default:
			if text := fmt.Sprintf("%v", value); text != "" && text != "<nil>" {
				meta[strings.ToLower(key)] = text
			}
		}
	}
	body = strings.TrimSpace(strings.Join(lines[end+1:], "\n"))
	return body, meta
}

// sanitizeZipPath 规范化 zip 内路径，拒绝路径穿越与绝对路径。
func sanitizeZipPath(name string) (string, bool) {
	normalized := strings.ReplaceAll(name, "\\", "/")
	cleanName := path.Clean(normalized)
	if cleanName == "." || strings.HasPrefix(cleanName, "../") || strings.HasPrefix(cleanName, "/") || path.IsAbs(cleanName) {
		return "", false
	}
	return cleanName, true
}

// locateSkillMarkdown 定位 SKILL.md 并计算包根目录：
// 根目录直接有 SKILL.md 时 rootDir 为空；否则要求所有文件位于同一顶层目录下。
func locateSkillMarkdown(entries []*zip.File) (string, *zip.File, error) {
	var rootCandidate *zip.File
	for _, file := range entries {
		base := path.Base(file.Name)
		if !strings.EqualFold(base, skillMarkdownFileName) {
			continue
		}
		dir := path.Dir(file.Name)
		if dir == "." {
			return "", file, nil
		}
		topDir := strings.Split(dir, "/")[0]
		if topDir != "" {
			rootCandidate = file
			break
		}
	}
	if rootCandidate == nil {
		return "", nil, ErrInvalidPackage
	}
	rootDir := strings.Split(path.Dir(rootCandidate.Name), "/")[0]
	for _, file := range entries {
		if file == rootCandidate {
			continue
		}
		if !strings.HasPrefix(file.Name, rootDir+"/") {
			return "", nil, ErrInvalidPackage
		}
	}
	return rootDir, rootCandidate, nil
}

// skillTitleFromRootDir 从包根目录名推导标题，如 "my-skill" → "My Skill"。
func skillTitleFromRootDir(rootDir string) string {
	fallback := strings.TrimSuffix(rootDir, ".skill")
	words := strings.FieldsFunc(fallback, func(r rune) bool {
		return r == '-' || r == '_' || r == ' '
	})
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	title := strings.Join(words, " ")
	if title == "" {
		return "Skill Package"
	}
	return title
}

// skillTriggerFromRootDir 从包根目录名推导触发词。
func skillTriggerFromRootDir(rootDir string) string {
	trigger := strings.ToLower(strings.TrimSuffix(rootDir, ".skill"))
	if trigger == "" {
		return "skill"
	}
	return trigger
}

func readZipEntry(file *zip.File, limit int64) ([]byte, error) {
	if int64(file.UncompressedSize64) > limit {
		return nil, ErrInvalidPackage
	}
	reader, err := file.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = reader.Close() }()
	return io.ReadAll(io.LimitReader(reader, limit+1))
}

var textExtensions = map[string]struct{}{
	".md": {}, ".markdown": {}, ".txt": {}, ".text": {}, ".rst": {},
	".py": {}, ".pyw": {}, ".js": {}, ".mjs": {}, ".cjs": {}, ".ts": {}, ".tsx": {}, ".jsx": {},
	".json": {}, ".jsonc": {}, ".yaml": {}, ".yml": {}, ".toml": {}, ".ini": {}, ".cfg": {}, ".conf": {},
	".csv": {}, ".tsv": {}, ".sh": {}, ".bash": {}, ".zsh": {}, ".fish": {}, ".ps1": {}, ".bat": {}, ".cmd": {},
	".go": {}, ".rs": {}, ".java": {}, ".kt": {}, ".c": {}, ".h": {}, ".cpp": {}, ".hpp": {}, ".cs": {}, ".rb": {},
	".php": {}, ".lua": {}, ".pl": {}, ".r": {}, ".sql": {}, ".html": {}, ".htm": {}, ".css": {}, ".scss": {}, ".less": {},
	".xml": {}, ".xsl": {}, ".svg": {}, ".env": {}, ".example": {}, ".sample": {}, ".gitignore": {},
	".dockerfile": {}, ".lock": {}, ".log": {}, ".properties": {}, ".gradle": {},
}

var binaryExtensions = map[string]struct{}{
	".png": {}, ".jpg": {}, ".jpeg": {}, ".gif": {}, ".webp": {}, ".bmp": {}, ".ico": {}, ".tiff": {},
	".pdf": {}, ".doc": {}, ".docx": {}, ".xls": {}, ".xlsx": {}, ".ppt": {}, ".pptx": {},
	".zip": {}, ".tar": {}, ".gz": {}, ".bz2": {}, ".xz": {}, ".7z": {}, ".rar": {},
	".wasm": {}, ".woff": {}, ".woff2": {}, ".ttf": {}, ".otf": {}, ".eot": {},
	".mp3": {}, ".mp4": {}, ".wav": {}, ".ogg": {}, ".webm": {}, ".mov": {}, ".avi": {},
	".exe": {}, ".dll": {}, ".so": {}, ".dylib": {}, ".bin": {}, ".dat": {}, ".db": {}, ".sqlite": {}, ".sqlite3": {},
}

// isLikelyText 判断 zip 条目是否为文本文件：扩展名命中二进制黑名单返回 false；
// 扩展名命中文本白名单返回 true；否则嗅探内容是否 UTF-8。
func isLikelyText(file *zip.File) bool {
	ext := strings.ToLower(path.Ext(file.Name))
	if _, ok := binaryExtensions[ext]; ok {
		return false
	}
	if _, ok := textExtensions[ext]; ok {
		return true
	}
	if file.UncompressedSize64 > maxPackageInspectBytes {
		return false
	}
	reader, err := file.Open()
	if err != nil {
		return false
	}
	defer func() { _ = reader.Close() }()
	head, err := io.ReadAll(io.LimitReader(reader, maxPackageInspectBytes))
	if err != nil {
		return false
	}
	return utf8.Valid(head)
}

// packageFileRecord 是包文件清单的持久化形态（领域类型不含 JSON 契约，存储层负责映射）。
type packageFileRecord struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Kind string `json:"kind"`
}

// encodePackageFilesJSON 序列化包文件清单，供持久化。
func encodePackageFilesJSON(files []domainskill.PackageFile) string {
	if len(files) == 0 {
		return ""
	}
	records := make([]packageFileRecord, 0, len(files))
	for _, file := range files {
		records = append(records, packageFileRecord{Path: file.Path, Size: file.Size, Kind: file.Kind})
	}
	data, err := json.Marshal(records)
	if err != nil {
		return ""
	}
	return string(data)
}

// decodePackageFilesJSON 反序列化包文件清单。
func decodePackageFilesJSON(raw string) []domainskill.PackageFile {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var records []packageFileRecord
	if err := json.Unmarshal([]byte(raw), &records); err != nil {
		return nil
	}
	files := make([]domainskill.PackageFile, 0, len(records))
	for _, record := range records {
		files = append(files, domainskill.PackageFile{Path: record.Path, Size: record.Size, Kind: record.Kind})
	}
	return files
}
