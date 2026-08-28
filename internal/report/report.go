// Package report 定义体检结果的数据模型：一次体检 = 若干工具 × 若干检查项。
//
// 这里只有数据和汇总逻辑，不负责打印（见 ui）也不负责检查（见 harness）。
// JSON 形态是给 agent 用的稳定契约：`aivet check --json`。
package report

import "time"

// Status 是单条检查的结论。
type Status string

const (
	OK   Status = "ok"   // 通过
	Warn Status = "warn" // 能用，但有隐患或没验到底
	Fail Status = "fail" // 用不了，必须处理
	Skip Status = "skip" // 没查（工具没装 / 用户关掉了 / 离线）
)

// Check 是一条检查结果。
type Check struct {
	ID     string `json:"id"`               // 稳定标识，如 codex.wire_api
	Tool   string `json:"tool"`             // 归属工具 id
	Title  string `json:"title"`            // 一句话说明查的是什么
	Status Status `json:"status"`           //
	Detail string `json:"detail,omitempty"` // 观察到的事实（已脱敏）
	Hint   string `json:"hint,omitempty"`   // 给人看的下一步建议
	FixID  string `json:"fix,omitempty"`    // 可自动修复时对应的 fixer id
}

// Tool 是一个工具的体检汇总。
type Tool struct {
	ID        string  `json:"id"`
	Label     string  `json:"label"`
	Installed bool    `json:"installed"`
	Path      string  `json:"path,omitempty"`
	Version   string  `json:"version,omitempty"`
	Checks    []Check `json:"checks"`
}

// Report 是一次完整体检。
type Report struct {
	AivetVersion string    `json:"aivet_version"`
	OS           string    `json:"os"`
	Arch         string    `json:"arch"`
	Time         time.Time `json:"time"`
	Live         bool      `json:"live"`
	Tools        []Tool    `json:"tools"`
}

// Counts 汇总各状态数量。
type Counts struct {
	OK, Warn, Fail, Skip int
}

// Count 统计一个工具的检查项状态。
func (t Tool) Count() Counts {
	return countChecks(t.Checks)
}

// Worst 返回该工具最差的状态（用于标题着色）。
func (t Tool) Worst() Status {
	if !t.Installed {
		return Skip
	}
	return worst(t.Checks)
}

// Count 统计整份报告。
func (r Report) Count() Counts {
	var c Counts
	for _, t := range r.Tools {
		tc := t.Count()
		c.OK += tc.OK
		c.Warn += tc.Warn
		c.Fail += tc.Fail
		c.Skip += tc.Skip
	}
	return c
}

// ExitCode：有 fail → 1，否则 0。warn 不影响退出码——「能用」就是能用。
func (r Report) ExitCode() int {
	if r.Count().Fail > 0 {
		return 1
	}
	return 0
}

// Fixable 列出报告里所有带 fixer 的检查项。
func (r Report) Fixable() []Check {
	var out []Check
	for _, t := range r.Tools {
		for _, c := range t.Checks {
			if c.FixID != "" && c.Status != OK {
				out = append(out, c)
			}
		}
	}
	return out
}

// Problems 列出所有 fail/warn 的检查项（给 agent 交接用）。
func (r Report) Problems() []Check {
	var out []Check
	for _, t := range r.Tools {
		for _, c := range t.Checks {
			if c.Status == Fail || c.Status == Warn {
				out = append(out, c)
			}
		}
	}
	return out
}

func countChecks(cs []Check) Counts {
	var c Counts
	for _, ch := range cs {
		switch ch.Status {
		case OK:
			c.OK++
		case Warn:
			c.Warn++
		case Fail:
			c.Fail++
		case Skip:
			c.Skip++
		}
	}
	return c
}

func worst(cs []Check) Status {
	w := OK
	for _, c := range cs {
		switch c.Status {
		case Fail:
			return Fail
		case Warn:
			w = Warn
		}
	}
	return w
}
