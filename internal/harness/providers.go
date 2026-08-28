package harness

import "github.com/shiftu/aivet/internal/probe"

// KnownProvider 描述 hermes / pi / dsh 内置的那些「官方」提供方：
// 它们不写 base_url，key 从固定名字的环境变量来。
//
// 事实本身住在 internal/knowledge —— 那里能被用户的 ~/.aivet/knowledge.json
// 覆盖。这里只负责把它翻译成 probe 认识的类型。
type KnownProvider struct {
	EnvKeys  []string
	BaseURL  string
	Protocol probe.Protocol
}

// Provider 查提供方；找不到返回 false。
//
// 挂在 Context 上而不是包级函数，是因为用户可以改写这份知识 ——
// 拿不到 Context 就拿不到那份改写，只会用内置的那套去核对，然后报错的结论。
func (c *Context) Provider(name string) (KnownProvider, bool) {
	p, ok := c.K().Provider(name)
	if !ok {
		return KnownProvider{}, false
	}
	return KnownProvider{EnvKeys: p.EnvKeys, BaseURL: p.BaseURL, Protocol: probe.Protocol(p.Protocol)}, true
}

// FirstEnv 依次读一组环境变量，返回第一个非空的 (名字, 值)。
func FirstEnv(env func(string) string, names ...string) (string, string) {
	for _, n := range names {
		if v := env(n); v != "" {
			return n, v
		}
	}
	return "", ""
}
