package harness

import "github.com/shiftu/aivet/internal/report"

// Builder 让各工具用一致的方式攒检查项，少写重复代码。
type Builder struct {
	tool   string
	checks []report.Check
}

// NewBuilder 建一个归属 tool 的 builder。
func NewBuilder(tool string) *Builder { return &Builder{tool: tool} }

func (b *Builder) add(id, title string, st report.Status, detail, hint, fix string) {
	b.checks = append(b.checks, report.Check{ID: b.tool + "." + id, Tool: b.tool, Title: title, Status: st, Detail: detail, Hint: hint, FixID: fix})
}

// OK 记一条通过。
func (b *Builder) OK(id, title, detail string) { b.add(id, title, report.OK, detail, "", "") }

// Warn 记一条提醒。
func (b *Builder) Warn(id, title, detail, hint string) {
	b.add(id, title, report.Warn, detail, hint, "")
}

// Fail 记一条故障。
func (b *Builder) Fail(id, title, detail, hint string) {
	b.add(id, title, report.Fail, detail, hint, "")
}

// FailFix 记一条可自动修复的故障。
func (b *Builder) FailFix(id, title, detail, hint, fix string) {
	b.add(id, title, report.Fail, detail, hint, fix)
}

// WarnFix 记一条可自动修复的提醒。
func (b *Builder) WarnFix(id, title, detail, hint, fix string) {
	b.add(id, title, report.Warn, detail, hint, fix)
}

// Skip 记一条跳过。
func (b *Builder) Skip(id, title, detail string) { b.add(id, title, report.Skip, detail, "", "") }

// Checks 取结果。
func (b *Builder) Checks() []report.Check { return b.checks }

// HasFail 判断目前为止有没有故障（用于决定要不要继续做更贵的探测）。
func (b *Builder) HasFail() bool {
	for _, c := range b.checks {
		if c.Status == report.Fail {
			return true
		}
	}
	return false
}
