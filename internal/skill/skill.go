// Package skill 把 aivet 作为「技能」装进各 agent，让它们知道该怎么调用 aivet。
package skill

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed SKILL.md
var Content string

// Targets 是各 agent 的用户级 skills 目录（相对 home）。
var Targets = map[string]string{
	"claude": filepath.Join(".claude", "skills", "aivet"),
	"codex":  filepath.Join(".codex", "skills", "aivet"),
	"hermes": filepath.Join(".hermes", "skills", "aivet"),
	"pi":     filepath.Join(".pi", "agent", "skills", "aivet"),
}

// Install 写入 SKILL.md；返回写到了哪。
func Install(home, tool string) (string, error) {
	rel, ok := Targets[tool]
	if !ok {
		return "", os.ErrNotExist
	}
	dir := filepath.Join(home, rel)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, "SKILL.md")
	return p, os.WriteFile(p, []byte(Content), 0o644)
}
