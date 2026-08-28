package harness

import "github.com/shiftu/aivet/internal/probe"

// KnownProvider 描述 hermes / pi / dsh 内置的那些「官方」提供方：
// 它们不写 base_url，key 从固定名字的环境变量来。
type KnownProvider struct {
	EnvKeys  []string
	BaseURL  string
	Protocol probe.Protocol
}

var knownProviders = map[string]KnownProvider{
	"openai":            {[]string{"OPENAI_API_KEY"}, "https://api.openai.com/v1", probe.ChatCompletions},
	"anthropic":         {[]string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN"}, "https://api.anthropic.com", probe.Anthropic},
	"deepseek":          {[]string{"DEEPSEEK_API_KEY"}, "https://api.deepseek.com/v1", probe.ChatCompletions},
	"openrouter":        {[]string{"OPENROUTER_API_KEY"}, "https://openrouter.ai/api/v1", probe.ChatCompletions},
	"google":            {[]string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, "", ""},
	"gemini":            {[]string{"GEMINI_API_KEY", "GOOGLE_API_KEY"}, "", ""},
	"groq":              {[]string{"GROQ_API_KEY"}, "https://api.groq.com/openai/v1", probe.ChatCompletions},
	"mistral":           {[]string{"MISTRAL_API_KEY"}, "https://api.mistral.ai/v1", probe.ChatCompletions},
	"xai":               {[]string{"XAI_API_KEY"}, "https://api.x.ai/v1", probe.ChatCompletions},
	"zai":               {[]string{"ZAI_API_KEY", "GLM_API_KEY"}, "https://api.z.ai/api/paas/v4", probe.ChatCompletions},
	"minimax":           {[]string{"MINIMAX_API_KEY"}, "https://api.minimax.io/v1", probe.ChatCompletions},
	"minimax-cn":        {[]string{"MINIMAX_API_KEY", "MINIMAX_CN_API_KEY"}, "https://api.minimaxi.com/v1", probe.ChatCompletions},
	"kimi":              {[]string{"MOONSHOT_API_KEY", "KIMI_API_KEY"}, "https://api.moonshot.cn/v1", probe.ChatCompletions},
	"moonshot":          {[]string{"MOONSHOT_API_KEY"}, "https://api.moonshot.cn/v1", probe.ChatCompletions},
	"qwen":              {[]string{"DASHSCOPE_API_KEY"}, "https://dashscope.aliyuncs.com/compatible-mode/v1", probe.ChatCompletions},
	"dashscope":         {[]string{"DASHSCOPE_API_KEY"}, "https://dashscope.aliyuncs.com/compatible-mode/v1", probe.ChatCompletions},
	"deepseek-official": {[]string{"DEEPSEEK_API_KEY"}, "https://api.deepseek.com/v1", probe.ChatCompletions},
}

// LookupProvider 查内置提供方；找不到返回 false。
func LookupProvider(name string) (KnownProvider, bool) {
	p, ok := knownProviders[name]
	return p, ok
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
