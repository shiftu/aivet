package probe

import (
	"regexp"
	"strings"
)

// TOML 行级编辑：codex 的 config.toml 是用户手写的、带注释的文件，
// 反序列化再序列化会把注释和顺序全冲掉。这里只动要动的那几行。

// TOMLSetTopKey 设置文件顶部（第一个 [table] 之前）的一个 key。
// 已存在则原地替换，否则插到顶部区域末尾。value 需已是 TOML 字面量（含引号）。
func TOMLSetTopKey(content, key, value string) string {
	lines := strings.Split(content, "\n")
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	topEnd := len(lines)
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "[") {
			topEnd = i
			break
		}
	}
	for i := 0; i < topEnd; i++ {
		if re.MatchString(lines[i]) {
			lines[i] = key + " = " + value
			return strings.Join(lines, "\n")
		}
	}
	ins := key + " = " + value
	head := append([]string{}, lines[:topEnd]...)
	// 顶部区域末尾若有空行，插在空行之前，保持和下一个表之间的空行。
	for len(head) > 0 && strings.TrimSpace(head[len(head)-1]) == "" {
		head = head[:len(head)-1]
	}
	rest := lines[topEnd:]
	out := append(head, ins)
	if len(rest) > 0 {
		out = append(out, "")
		out = append(out, rest...)
	}
	return strings.Join(out, "\n")
}

// TOMLSetTableKey 在指定表 [name] 内设置一个 key；表不存在则追加整张表。
func TOMLSetTableKey(content, table, key, value string) string {
	lines := strings.Split(content, "\n")
	start, end := tomlTableRange(lines, table)
	if start < 0 {
		body := "[" + table + "]\n" + key + " = " + value + "\n"
		return TOMLReplaceTable(content, table, body)
	}
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=`)
	for i := start + 1; i < end; i++ {
		if re.MatchString(lines[i]) {
			lines[i] = key + " = " + value
			return strings.Join(lines, "\n")
		}
	}
	// 插到表内最后一个非空行之后
	ins := end
	for ins > start+1 && strings.TrimSpace(lines[ins-1]) == "" {
		ins--
	}
	out := append([]string{}, lines[:ins]...)
	out = append(out, key+" = "+value)
	out = append(out, lines[ins:]...)
	return strings.Join(out, "\n")
}

// TOMLReplaceTable 整体替换 [table] 段（含头）；不存在则追加到文件末尾。
func TOMLReplaceTable(content, table, body string) string {
	lines := strings.Split(content, "\n")
	start, end := tomlTableRange(lines, table)
	body = strings.TrimRight(body, "\n")
	if start < 0 {
		c := strings.TrimRight(content, "\n")
		if c == "" {
			return body + "\n"
		}
		return c + "\n\n" + body + "\n"
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, strings.Split(body, "\n")...)
	if end < len(lines) {
		out = append(out, "")
		out = append(out, lines[end:]...)
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n"
}

// TOMLGetTableKey 读取 [table] 内 key 的字符串值（去引号），找不到返回空。
func TOMLGetTableKey(content, table, key string) string {
	lines := strings.Split(content, "\n")
	start, end := tomlTableRange(lines, table)
	if start < 0 {
		return ""
	}
	re := regexp.MustCompile(`^\s*` + regexp.QuoteMeta(key) + `\s*=\s*(.+?)\s*(#.*)?$`)
	for i := start + 1; i < end; i++ {
		if m := re.FindStringSubmatch(lines[i]); m != nil {
			return strings.Trim(m[1], `"'`)
		}
	}
	return ""
}

// tomlTableRange 返回 [table] 头所在行和段结束行（下一个表头或 EOF）。
func tomlTableRange(lines []string, table string) (int, int) {
	want := "[" + table + "]"
	wantQ := "[" + quoteTableParts(table) + "]"
	start := -1
	for i, l := range lines {
		t := strings.TrimSpace(l)
		if t == want || t == wantQ {
			start = i
			break
		}
	}
	if start < 0 {
		return -1, -1
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		t := strings.TrimSpace(lines[i])
		if strings.HasPrefix(t, "[") && !strings.HasPrefix(t, "[[") || strings.HasPrefix(t, "[[") {
			end = i
			break
		}
	}
	return start, end
}

// quoteTableParts 把 a.b-c 变成 a."b-c"（带连字符的段在 TOML 里常被引号包起来）。
func quoteTableParts(table string) string {
	parts := strings.Split(table, ".")
	for i, p := range parts {
		if strings.ContainsAny(p, "-. ") {
			parts[i] = `"` + p + `"`
		}
	}
	return strings.Join(parts, ".")
}
