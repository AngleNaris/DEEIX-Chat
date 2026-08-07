package conversation

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	domainskill "github.com/DEEIX-AI/DEEIX-Chat/backend/internal/domain/skill"
	"github.com/DEEIX-AI/DEEIX-Chat/backend/internal/infra/llm"
)

// 请求式披露常量：模型通过在回复中输出 <read_file path="..."> 标记请求技能包内文件内容，
// 后端检测后在下一轮 LLM 调用前补充注入文件内容。
const (
	// readFileTagOpen 是 read_file 标记的固定前缀。
	readFileTagOpen = "<read_file"
	// maxReadFileTagLength 单个 read_file 标记的最大字节长度（含路径），超出视为普通文本。
	maxReadFileTagLength = 512
	// maxReadFileRequestsPerTurn 每轮最多处理的 read_file 请求数。
	maxReadFileRequestsPerTurn = 8
	// skillFileReadLimitBytes 单文件最多注入模型的字节数（超出截断）。
	skillFileReadLimitBytes = 32 << 10
	// skillFileReadBudgetBytes 单轮注入所有技能文件的总字节预算。
	skillFileReadBudgetBytes = 64 << 10
)

// skillFileRequest 表示模型通过 <read_file> 标记请求的包内文件。
type skillFileRequest struct {
	Path string
}

// readFileTagScanner 从流式文本中过滤 read_file 标记并提取请求。
// 标记可能跨多个流式分片，因此需要缓冲未决的标记前缀。
type readFileTagScanner struct {
	buf      []byte
	requests []skillFileRequest
}

func newReadFileTagScanner() *readFileTagScanner {
	return &readFileTagScanner{}
}

// consume 处理新的文本分片，返回应展示的可见文本（标记被剥离）和本次新增的文件请求。
func (s *readFileTagScanner) consume(delta string) (string, []skillFileRequest) {
	if delta == "" {
		return "", nil
	}
	data := append(s.buf, delta...)
	s.buf = s.buf[:0]
	var visible strings.Builder
	for len(data) > 0 {
		prefix := bytes.Index(data, []byte(readFileTagOpen))
		if prefix < 0 {
			// 无标记前缀：检查 data 尾部是否为跨分片截断的标记前缀。
			if cut := partialReadFileTagPrefixLen(data); cut > 0 {
				visible.Write(data[:len(data)-cut])
				s.buf = append(s.buf, data[len(data)-cut:]...)
			} else {
				visible.Write(data)
			}
			break
		}
		if prefix > 0 {
			visible.Write(data[:prefix])
			data = data[prefix:]
			continue
		}
		if closeIdx := bytes.IndexByte(data, '>'); closeIdx >= 0 {
			tag := data[:closeIdx+1]
			data = data[closeIdx+1:]
			if req, ok := parseReadFileTag(tag); ok {
				s.requests = append(s.requests, req)
			} else {
				visible.Write(tag)
			}
			continue
		}
		// 未闭合：缓冲等待后续分片，超过最大合法标记长度则按普通文本输出。
		if len(data) > maxReadFileTagLength {
			if cut := partialReadFileTagPrefixLen(data); cut > 0 {
				visible.Write(data[:len(data)-cut])
				s.buf = append(s.buf, data[len(data)-cut:]...)
			} else {
				visible.Write(data)
			}
		} else {
			s.buf = append(s.buf, data...)
		}
		break
	}
	requests := s.requests
	s.requests = nil
	return visible.String(), requests
}

// partialReadFileTagPrefixLen 返回 data 尾部与 <read_file 前缀匹配的最长截断长度（1..len(data)-1）。
func partialReadFileTagPrefixLen(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	limit := len(readFileTagOpen) - 1
	if len(data) < limit {
		limit = len(data)
	}
	for n := limit; n >= 1; n-- {
		if bytes.HasSuffix(data, []byte(readFileTagOpen[:n])) {
			return n
		}
	}
	return 0
}

// parseReadFileTag 解析 <read_file path="...">（或 <read_file path="..."/>）标记。
func parseReadFileTag(tag []byte) (skillFileRequest, bool) {
	rest := bytes.TrimSpace(bytes.TrimPrefix(tag, []byte(readFileTagOpen)))
	if !bytes.HasPrefix(rest, []byte("path")) {
		return skillFileRequest{}, false
	}
	rest = bytes.TrimSpace(rest[len("path"):])
	if len(rest) == 0 || rest[0] != '=' {
		return skillFileRequest{}, false
	}
	rest = bytes.TrimSpace(rest[1:])
	if len(rest) < 2 || rest[0] != '"' {
		return skillFileRequest{}, false
	}
	end := bytes.IndexByte(rest[1:], '"')
	if end < 0 {
		return skillFileRequest{}, false
	}
	path := rest[1 : 1+end]
	if strings.TrimSpace(string(path)) == "" {
		return skillFileRequest{}, false
	}
	rest = bytes.TrimSpace(rest[1+end+1:])
	if len(rest) == 0 || (rest[0] != '>' && !(rest[0] == '/' && len(rest) >= 2 && rest[1] == '>')) {
		return skillFileRequest{}, false
	}
	return skillFileRequest{Path: string(path)}, true
}

// resolveSkillFileRequests 根据 read_file 请求读取选中技能包内文件内容并构造补充 system 消息。
// path 必须命中某个选中包技能的清单（白名单校验在 GetPackageFile 内再次执行），
// 无效路径或读取失败的文件被静默跳过；总注入字节数受 skillFileReadBudgetBytes 预算约束。
// 返回补充消息与成功读取的文件路径列表。
func (s *Service) resolveSkillFileRequests(ctx context.Context, userID uint, prompt *skillPrompts, requests []skillFileRequest) ([]llm.Message, []string) {
	if prompt == nil || len(prompt.Skills) == 0 || len(requests) == 0 || s.skillResolver == nil {
		return nil, nil
	}
	if len(requests) > maxReadFileRequestsPerTurn {
		requests = requests[:maxReadFileRequestsPerTurn]
	}
	skills := make([]map[string]domainskill.PackageFile, 0, len(prompt.Skills))
	skillIDs := make([]uint, 0, len(prompt.Skills))
	for _, skill := range prompt.Skills {
		if !skill.IsPackage() || len(skill.PackageFiles) == 0 {
			continue
		}
		index := make(map[string]domainskill.PackageFile, len(skill.PackageFiles))
		for _, file := range skill.PackageFiles {
			index[file.Path] = file
		}
		skills = append(skills, index)
		skillIDs = append(skillIDs, skill.ID)
	}
	if len(skills) == 0 {
		return nil, nil
	}
	var messages []llm.Message
	var loaded []string
	budget := skillFileReadBudgetBytes
	for _, req := range requests {
		if budget <= 0 {
			break
		}
		skillID, ok := matchPackageFileOwner(skills, skillIDs, req.Path)
		if !ok {
			continue
		}
		content, err := s.skillResolver.GetPackageFile(ctx, userID, skillID, req.Path)
		if err != nil {
			continue
		}
		if len(content) > skillFileReadLimitBytes {
			content = content[:skillFileReadLimitBytes]
		}
		budget -= len(content)
		loaded = append(loaded, req.Path)
		messages = append(messages, llm.Message{
			Role: "system",
			Content: fmt.Sprintf(
				"<skill_file_content skill_id=\"%d\" path=\"%s\">\n<content>\n%s\n</content>\n</skill_file_content>",
				skillID,
				xmlEscapeAttr(req.Path),
				xmlEscapeText(string(content)),
			),
		})
	}
	return messages, loaded
}

// matchPackageFileOwner 返回 path 所属的第一个选中包技能 ID。
func matchPackageFileOwner(skills []map[string]domainskill.PackageFile, skillIDs []uint, path string) (uint, bool) {
	for i, index := range skills {
		if _, ok := index[path]; ok {
			return skillIDs[i], true
		}
	}
	return 0, false
}
