package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shiftu/aivet/internal/ui"
)

// Renderer 把命令说明画到终端上。
type Renderer struct {
	W       io.Writer
	P       ui.Palette
	Wid     int
	Version string
}

func (r Renderer) title(s string) { fmt.Fprintf(r.W, "\n  %s\n", r.P.Bold(s)) }
func (r Renderer) body(s, ind string) {
	for _, line := range strings.Split(s, "\n") {
		if line == "" {
			fmt.Fprintln(r.W)
			continue
		}
		fmt.Fprintf(r.W, "%s%s\n", ind, ui.Wrap(line, r.Wid-len(ind)-2, len(ind)))
	}
}

// Overview 画命令总览。
func (r Renderer) Overview(cmds []Command) {
	fmt.Fprintf(r.W, "\n%s %s  %s\n", r.P.Bold(r.P.Cyan("aivet")),
		r.P.Dim("v"+strings.TrimPrefix(r.Version, "v")), r.P.Dim("— "+Tagline))
	fmt.Fprintf(r.W, "\n  %s  %s\n", r.P.Dim("用法"), "aivet <命令> [参数…] [选项]")

	for _, g := range Groups {
		var in []Command
		for _, c := range cmds {
			if c.Group == g {
				in = append(in, c)
			}
		}
		if len(in) == 0 {
			continue
		}
		r.title(g)
		for _, c := range in {
			fmt.Fprintf(r.W, "    %s  %s\n", r.P.Cyan(ui.Pad(c.Name, 9)), c.Summary)
		}
	}

	r.title("退出码")
	for _, k := range []string{"0", "1", "2"} {
		fmt.Fprintf(r.W, "    %s  %s\n", r.P.Dim(ui.Pad(k, 9)), ExitCodes[k])
	}

	fmt.Fprintln(r.W)
	for _, t := range []struct{ who, cmd, why string }{
		{"第一次用？", "aivet setup", "问三样东西，把所有工具一次配好"},
		{"想看某个命令", "aivet help check", "每个命令都有详细页"},
		{"你是 agent？", "aivet help --json", "完整规格，含报告结构"},
	} {
		fmt.Fprintf(r.W, "  %s  %s  %s\n", r.P.Dim(ui.Pad(t.who, 14)), r.P.Bold(ui.Pad(t.cmd, 18)), r.P.Dim("—— "+t.why))
	}
	fmt.Fprintln(r.W)
}

// Detail 画单个命令的详细页。
func (r Renderer) Detail(c Command) {
	fmt.Fprintf(r.W, "\n%s  %s\n", r.P.Bold(r.P.Cyan("aivet "+c.Name)), r.P.Dim("— "+c.Summary))
	if len(c.Aliases) > 0 {
		fmt.Fprintf(r.W, "%s\n", r.P.Dim("  别名："+strings.Join(c.Aliases, "、")))
	}
	fmt.Fprintf(r.W, "\n  %s  %s\n", r.P.Dim("用法"), c.Usage)
	if c.Args != "" {
		fmt.Fprintf(r.W, "  %s  %s\n", r.P.Dim("参数"), c.Args)
	}
	if c.Long != "" {
		fmt.Fprintln(r.W)
		r.body(c.Long, "  ")
	}
	if len(c.Flags) > 0 {
		r.title("选项")
		w := 0
		for _, f := range c.Flags {
			if n := ui.DisplayWidth(flagLabel(f)); n > w {
				w = n
			}
		}
		for _, f := range c.Flags {
			fmt.Fprintf(r.W, "    %s  %s\n", r.P.Cyan(ui.Pad(flagLabel(f), w)), ui.Wrap(f.Desc, r.Wid-w-8, w+6))
			if f.Env != "" {
				fmt.Fprintf(r.W, "    %s  %s\n", strings.Repeat(" ", w), r.P.Dim("也可以用环境变量 "+f.Env))
			}
		}
	}
	if len(c.Examples) > 0 {
		r.title("例子")
		w := 0
		for _, e := range c.Examples {
			if n := ui.DisplayWidth(e.Cmd); n > w {
				w = n
			}
		}
		if w > r.Wid-30 {
			w = 0 // 命令太长就换行写，别把说明挤到屏幕外
		}
		for _, e := range c.Examples {
			if w == 0 {
				fmt.Fprintf(r.W, "    %s\n      %s\n", r.P.Bold(e.Cmd), r.P.Dim(e.Desc))
				continue
			}
			fmt.Fprintf(r.W, "    %s  %s\n", r.P.Bold(ui.Pad(e.Cmd, w)), r.P.Dim(e.Desc))
		}
	}
	if len(c.Notes) > 0 {
		r.title("注意")
		for _, n := range c.Notes {
			fmt.Fprintf(r.W, "    %s %s\n", r.P.Dim("·"), ui.Wrap(n, r.Wid-8, 6))
		}
	}
	fmt.Fprintln(r.W)
}

func flagLabel(f Flag) string {
	s := "--" + f.Name
	if f.Arg != "" {
		s += " " + f.Arg
	}
	return s
}

// spec 是 `aivet help --json` 的输出结构：agent 一次拿到所有该知道的东西。
type spec struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Summary      string            `json:"summary"`
	Tools        []string          `json:"tools"`
	ExitCodes    map[string]string `json:"exit_codes"`
	Commands     []Command         `json:"commands"`
	ReportSchema reportSchema      `json:"report_schema"`
}

type reportSchema struct {
	Describes string            `json:"describes"`
	SavedTo   string            `json:"saved_to"`
	Statuses  map[string]string `json:"check_status_values"`
	Fields    map[string]string `json:"check_fields"`
	Workflow  []string          `json:"suggested_agent_workflow"`
}

// JSON 把完整规格写成 JSON。
func (r Renderer) JSON(cmds []Command) error {
	s := spec{
		Name: "aivet", Version: r.Version, Summary: Tagline,
		Tools: Tools, ExitCodes: ExitCodes, Commands: cmds,
		ReportSchema: reportSchema{
			Describes: "`aivet check --json` 的输出：{aivet_version, os, arch, time, live, tools:[{id, label, installed, path, version, checks:[…]}]}",
			SavedTo:   "~/.aivet/last-report.json（每次 check / ask 都会刷新）",
			Statuses: map[string]string{
				"ok":   "这一项没问题",
				"warn": "能用，但有隐患，或 aivet 查不到底（不影响退出码）",
				"fail": "用不了，必须处理（退出码变 1）",
				"skip": "没查（工具没装 / --offline / 该项不适用）",
			},
			Fields: map[string]string{
				"id":     "稳定标识，如 codex.wire_api，可用于跨次比对",
				"tool":   "归属工具 id",
				"title":  "查的是什么",
				"status": "ok / warn / fail / skip",
				"detail": "观察到的事实，key 已脱敏",
				"hint":   "给人看的下一步建议",
				"fix":    "非空表示可以用 `aivet fix <这个值>` 自动修",
			},
			Workflow: []string{
				"1. aivet check --json —— 拿到结构化结果，只处理 status=fail 的项",
				"2. 带 fix 字段的：aivet fix --yes（会自动备份原文件）",
				"3. 不带 fix 字段的：按 hint 处理；装软件前先问用户",
				"4. aivet check --json 复验，直到没有 fail",
				"5. 判断不了通不通的（hint 里提到 --live 时）：aivet check <工具> --live",
			},
		},
	}
	enc := json.NewEncoder(r.W)
	enc.SetIndent("", "  ")
	return enc.Encode(s)
}
