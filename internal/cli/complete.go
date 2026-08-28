package cli

import "strings"

// 这里是 shell 补全的大脑。
//
// 为什么补全逻辑写在 Go 里、而不是写进各家 shell 的脚本：候选项有一半是活的 ——
// `aivet fix` 能修哪几项由各工具的 Fixers() 决定，会随版本变。写死在 shell 脚本里，
// 用户升级 aivet 之后补全就开始撒谎，而且他不会发现（补全不报错，只是补错）。
// 所以四家 shell 的脚本都只是十行胶水，每敲一次 Tab 就回头问一次二进制本身。
//
// 附带的好处：补全和帮助共用 Commands() 这一份说明书，谁也跑不到对方前面去。

// 候选就用 Value —— 和说明书里描述取值的是同一个类型，说明也就跟着一起补出来了。
// zsh 和 fish 会把说明显示在候选旁边；bash 显示不了，在输出那一层丢掉。

// Complete 算出补全候选。
//
// words 是 aivet 后面的全部词，最后一个是光标所在的那个（可能是空串）。
// 约定「最后一个总是正在敲的词」，是因为四家 shell 取当前词的方式各不相同，
// 与其在每个脚本里各写一套，不如让脚本都按同一个形状把词递进来。
func Complete(cmds []Command, words []string) []Value {
	words = normalize(words)
	if len(words) == 0 {
		words = []string{""}
	}
	cur, done := words[len(words)-1], words[:len(words)-1]

	// 还没敲命令名。
	if len(done) == 0 {
		if strings.HasPrefix(cur, "-") {
			return match(globalFlags(), cur)
		}
		var out []Value
		for _, c := range cmds {
			out = append(out, Value{c.Name, c.Summary})
		}
		return match(out, cur)
	}

	c, ok := Lookup(cmds, done[0])
	if !ok {
		return nil
	}
	rest := done[1:]

	// 上一个词是个要取值的选项，那现在敲的就是它的值。
	if len(rest) > 0 {
		if f, ok := flagOf(c, rest[len(rest)-1]); ok && f.Arg != "" {
			return flagValues(f, cur)
		}
	}

	if strings.HasPrefix(cur, "-") {
		// --tools=cl 这种连写：等号左边决定候选，补出来的必须是整个词。
		if name, tail, found := strings.Cut(cur, "="); found {
			f, ok := flagOf(c, name)
			if !ok || f.Arg == "" {
				return nil
			}
			var out []Value
			for _, v := range flagValues(f, tail) {
				out = append(out, Value{name + "=" + v.Name, v.Desc})
			}
			return out
		}
		var out []Value
		for _, f := range c.Flags {
			out = append(out, Value{"--" + f.Name, f.Desc})
		}
		out = append(out, Value{"--help", "这个命令的详细用法"})
		return match(out, cur)
	}

	// 位置参数。
	used := positionalsTyped(c, rest)
	if len(used) > 0 && !c.Repeat {
		return nil // 只收一个，已经写过了
	}
	var out []Value
	for _, p := range c.Positional {
		if used[p.Name] {
			continue // 已经写过的不再提示
		}
		out = append(out, Value{p.Name, p.Desc})
	}
	return match(out, cur)
}

// globalFlags 是不跟在任何子命令后面的那几个。
func globalFlags() []Value {
	return []Value{
		{"--help", "命令总览"},
		{"--version", "打印版本号"},
	}
}

// flagValues 给一个选项的取值补全。逗号分隔的（--tools claude,codex）只补最后一段，
// 但补出来的要带上前面几段 —— 否则 shell 会把已经敲好的部分吃掉。
func flagValues(f Flag, cur string) []Value {
	if !f.List {
		return match(f.Values, cur)
	}
	i := strings.LastIndex(cur, ",")
	if i < 0 {
		return match(f.Values, cur)
	}
	head, tail := cur[:i+1], cur[i+1:]
	used := map[string]bool{}
	for _, s := range strings.Split(cur[:i], ",") {
		used[s] = true
	}
	var out []Value
	for _, v := range f.Values {
		if used[v.Name] || !strings.HasPrefix(v.Name, tail) {
			continue
		}
		out = append(out, Value{head + v.Name, v.Desc})
	}
	return out
}

// positionalsTyped 收集已经敲过的位置参数，跳过选项和它们的取值。
func positionalsTyped(c Command, rest []string) map[string]bool {
	used := map[string]bool{}
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if strings.HasPrefix(a, "-") {
			if f, ok := flagOf(c, a); ok && f.Arg != "" && !strings.Contains(a, "=") {
				i++ // 下一个词是它的值，不是位置参数
			}
			continue
		}
		used[a] = true
	}
	return used
}

// flagOf 按词找选项，-x / --x / --x=v 都认。
func flagOf(c Command, word string) (Flag, bool) {
	if !strings.HasPrefix(word, "-") {
		return Flag{}, false
	}
	name, _, _ := strings.Cut(strings.TrimLeft(word, "-"), "=")
	for _, f := range c.Flags {
		if f.Name == name {
			return f, true
		}
	}
	return Flag{}, false
}

// match 按前缀筛候选。
func match(items []Value, prefix string) []Value {
	var out []Value
	for _, v := range items {
		if strings.HasPrefix(v.Name, prefix) {
			out = append(out, v)
		}
	}
	return out
}

// normalize 把 bash 拆碎的词拼回去。
//
// bash 按 COMP_WORDBREAKS 断词，其中含 '='，所以 `--tools=cl` 递过来是三个词
// "--tools" "=" "cl"。zsh / fish 不拆。在这里统一成一个词，后面的逻辑就只有一种形状。
func normalize(words []string) []string {
	var out []string
	for _, w := range words {
		n := len(out)
		switch {
		case n > 0 && strings.HasPrefix(out[n-1], "-") && w == "=":
			out[n-1] += "="
		case n > 0 && strings.HasPrefix(out[n-1], "-") && strings.HasSuffix(out[n-1], "="):
			out[n-1] += w
		default:
			out = append(out, w)
		}
	}
	return out
}
