package integrate

import (
	"os"
	"path/filepath"
	"strings"
)

const gitignoreStartMarker = "# dwyt start"
const gitignoreEndMarker = "# dwyt end"

const gitignoreManagedBlock = "# dwyt start\n*mcp.json\n*opencode.json\n# dwyt end\n"

func EnsureGitignoreBlock(projectPath string) error {
	gitignorePath := filepath.Join(projectPath, ".gitignore")
	data, err := os.ReadFile(gitignorePath)
	if err != nil {
		if os.IsNotExist(err) {
			return os.WriteFile(gitignorePath, []byte(gitignoreManagedBlock), 0644)
		}
		return err
	}

	content := string(data)
	if hasGitignoreBlock(content) {
		newContent := updateGitignoreBlock(content)
		if newContent == content {
			return nil
		}
		return os.WriteFile(gitignorePath, []byte(newContent), 0644)
	}

	separator := ""
	if strings.TrimSpace(content) != "" {
		if !strings.HasSuffix(content, "\n") {
			separator = "\n\n"
		} else if !strings.HasSuffix(content, "\n\n") {
			separator = "\n"
		}
	}
	newContent := content + separator + gitignoreManagedBlock
	return os.WriteFile(gitignorePath, []byte(newContent), 0644)
}

func hasGitignoreBlock(content string) bool {
	return strings.Contains(content, gitignoreStartMarker) && strings.Contains(content, gitignoreEndMarker)
}

func updateGitignoreBlock(content string) string {
	var result strings.Builder
	result.Grow(len(content))

	offset := 0
	for {
		startIdx := strings.Index(content[offset:], gitignoreStartMarker)
		if startIdx < 0 {
			result.WriteString(content[offset:])
			break
		}
		startIdx += offset
		endIdx := strings.Index(content[startIdx:], gitignoreEndMarker)
		if endIdx < 0 {
			result.WriteString(content[offset:])
			break
		}
		endIdx = startIdx + endIdx + len(gitignoreEndMarker)
		if endIdx < len(content) && content[endIdx] == '\n' {
			endIdx++
		}

		result.WriteString(content[offset:startIdx])
		result.WriteString(gitignoreManagedBlock)
		offset = endIdx
	}

	return result.String()
}
