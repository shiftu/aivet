package harness

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// SchemaDrift 检查「文件读进来了、也有内容，但 aivet 认得的顶层键一个都没有」。
//
// 这是 aivet 最危险的失效方式，值得单开一个检查项。目标工具升级后改了配置结构
// （换键名、挪层级），解析器**不会报错** —— 它只是把每个字段留成零值。
// 于是后面每一条检查都在核对空值，然后一路绿灯：查了等于没查，
// 而报告上写着「一切正常」。这比解析失败危险得多，因为没人会来问。
//
// known 只列「这个文件存在就该有」的顶层键。挑不出这种键的文件（比如 Claude Code
// 的 settings.json，里面可能全是主题、权限之类与 aivet 无关的设置）不适用这个检查 ——
// 那里的误报会比它挡住的问题更吵。
//
// 返回 true 表示确实对不上。调用方通常应当就此打住：再往下的每一条检查
// 都建立在「我读懂了这个文件」的假设上，而这个假设刚刚被推翻了。
func SchemaDrift(b *Builder, tool, id, path string, raw map[string]any, known ...string) bool {
	if len(raw) == 0 {
		return false // 空文件是另一回事，交给各工具自己判断
	}
	for _, k := range known {
		if _, ok := raw[k]; ok {
			return false
		}
	}
	saw := make([]string, 0, len(raw))
	for k := range raw {
		saw = append(saw, k)
	}
	sort.Strings(saw)
	if len(saw) > 8 {
		saw = append(saw[:8:8], "…")
	}
	b.Warn(id, "配置结构",
		fmt.Sprintf("%s 有内容，但里面没有 aivet 认识的任何一项（它有：%s）", filepath.Base(path), strings.Join(saw, "、")),
		fmt.Sprintf("多半是这件工具升级后改了配置结构，而 aivet 的知识停在旧版 —— "+
			"底下的检查会因此查不到东西，别当成没问题。"+
			"要确认到底能不能用：aivet check %s --live（让工具自己跑一次，不依赖 aivet 读得懂配置）。"+
			"知道新结构长什么样的话，往 ~/.aivet/knowledge.json 补一条：aivet knowledge --init", tool))
	return true
}
