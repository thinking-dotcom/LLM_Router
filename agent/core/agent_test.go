package core

import (
	"os"
	"path/filepath"
	"testing"
)

// 用临时目录作为 data 目录，避免触碰真实用户 data。
func setTestData(t *testing.T) {
	t.Setenv("LLMROUTER_DATA", filepath.Join(t.TempDir(), "data"))
}

func TestSkillsCRUD(t *testing.T) {
	setTestData(t)
	if err := SaveSkill("test-skill", "---\ndescription: 测试技能\n---\n# Test\nhello"); err != "" {
		t.Fatalf("SaveSkill: %s", err)
	}
	skills := ListSkills()
	if len(skills) != 1 || skills[0].Name != "test-skill" {
		t.Fatalf("ListSkills mismatch: %+v", skills)
	}
	if skills[0].Desc != "测试技能" {
		t.Fatalf("desc mismatch: %q", skills[0].Desc)
	}
	if got := GetSkillContent("test-skill"); got == "" {
		t.Fatal("GetSkillContent empty")
	}
	if err := DeleteSkill("test-skill"); err != "" {
		t.Fatalf("DeleteSkill: %s", err)
	}
	if len(ListSkills()) != 0 {
		t.Fatal("skill not deleted")
	}
}

func TestMcpReplacePreservesOtherKeys(t *testing.T) {
	setTestData(t)
	mcpFile := filepath.Join(t.TempDir(), "settings.json")
	original := `{"model":"claude","mcpServers":{"old":{"type":"sse","url":"http://old"}}}`
	_ = os.MkdirAll(filepath.Dir(mcpFile), 0o755)
	_ = os.WriteFile(mcpFile, []byte(original), 0o644)

	servers := []MCPServer{{Name: "mcp-a", Type: "sse", URL: "http://a"}}
	if err := injectMcp(mcpFile, "", servers); err != nil {
		t.Fatalf("injectMcp: %v", err)
	}
	b, _ := os.ReadFile(mcpFile)
	content := string(b)
	// 替换语义：其余顶层键保留，mcpServers = 仅本次所选（旧服务被清掉）
	if !containsStr(content, `"model"`) || !containsStr(content, `"mcp-a"`) {
		t.Fatalf("mcp replace lost keys:\n%s", content)
	}
	if containsStr(content, `"old"`) {
		t.Fatalf("old server should be replaced:\n%s", content)
	}
}

func TestSkillsInjectUndo(t *testing.T) {
	setTestData(t)
	_ = SaveSkill("sk1", "# Skill One")
	_ = SaveSkill("sk2", "# Skill Two")

	targetDir := filepath.Join(t.TempDir(), "skills")
	if err := injectSkills(targetDir, ListSkills()); err != nil {
		t.Fatalf("injectSkills: %v", err)
	}
	for _, p := range []string{"sk1", "sk2"} {
		if _, err := os.Stat(filepath.Join(targetDir, p, "SKILL.md")); err != nil {
			t.Fatalf("injected skill %s missing: %v", p, err)
		}
	}
	if err := undoSkills(targetDir); err != nil {
		t.Fatalf("undoSkills: %v", err)
	}
	if _, err := os.Stat(filepath.Join(targetDir, "sk1")); !os.IsNotExist(err) {
		t.Fatal("undoSkills did not remove sk1")
	}
}

func TestPlaceholderNotInjectable(t *testing.T) {
	RegisterTarget(&placeholder{id: "zz-ph", name: "PH", format: "/oai"})
	res := SyncTargets([]TargetOp{{TargetID: "zz-ph", Action: "inject"}}, "http://127.0.0.1:3000")
	if len(res) == 0 || res[0].Error == "" {
		t.Fatalf("placeholder should return an error, got %+v", res)
	}
}

func TestUnknownTargetReportsError(t *testing.T) {
	res := SyncTargets([]TargetOp{{TargetID: "no-such-target", Action: "inject"}}, "")
	if len(res) == 0 || res[0].Error == "" {
		t.Fatalf("unknown target should error, got %+v", res)
	}
}

func containsStr(hay, needle string) bool {
	if len(needle) == 0 {
		return true
	}
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
