package knowledge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, home, body string) {
	t.Helper()
	p := UserPath(home)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinLoadsWithoutUserFile(t *testing.T) {
	k := Load(t.TempDir())
	if k.Loaded {
		t.Error("没有用户文件却报告加载了")
	}
	if k.LoadErr != nil {
		t.Errorf("用户文件不存在是正常情况，不该算错：%v", k.LoadErr)
	}
	if _, ok := k.Provider("deepseek"); !ok {
		t.Error("内置提供方丢了")
	}
	if _, ok := k.AliasEnv("sonnet"); !ok {
		t.Error("内置模型别名丢了")
	}
}

func TestUserPatchOverridesAndAdds(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{
	  "providers": {
	    "kimi": {"env_keys": ["NEW_KEY"], "base_url": "https://new.example/v1", "protocol": "chat"},
	    "brandnew": {"env_keys": ["BN_KEY"], "base_url": "https://bn.example/v1", "protocol": "responses"}
	  },
	  "claude_model_aliases": {"fable": "ANTHROPIC_DEFAULT_FABLE_MODEL"},
	  "versions": {"codex.wire_api_chat_removed": "9.9.9"}
	}`)
	k := Load(home)
	if !k.Loaded {
		t.Fatal("用户文件没被加载")
	}
	if p, _ := k.Provider("kimi"); p.BaseURL != "https://new.example/v1" {
		t.Errorf("覆盖内置提供方失败：%+v", p)
	}
	if p, ok := k.Provider("brandnew"); !ok || p.Protocol != "responses" {
		t.Errorf("新增提供方失败：%+v", p)
	}
	// 没被提到的内置项必须原样保留 —— 用户只写想改的那几条，别的不该消失。
	if p, ok := k.Provider("deepseek"); !ok || p.BaseURL == "" {
		t.Error("用户补丁把没提到的内置提供方冲掉了")
	}
	if env, ok := k.AliasEnv("fable"); !ok || env != "ANTHROPIC_DEFAULT_FABLE_MODEL" {
		t.Error("新增模型别名失败")
	}
	if _, ok := k.AliasEnv("sonnet"); !ok {
		t.Error("内置别名被冲掉了")
	}
	if k.Version("codex.wire_api_chat_removed") != "9.9.9" {
		t.Error("版本断言没被覆盖")
	}
}

// 用户加一条新的配置路径时，内置的那条必须留着当兜底 ——
// 否则用户为了加一个位置就得把原来的抄一遍，抄漏了反而更糟。
func TestUserPathsPrependAndKeepBuiltin(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{"paths": {"claude.settings": ["~/somewhere/new.json"]}}`)
	k := Load(home)
	cands := k.Candidates("claude.settings")
	if len(cands) != 2 {
		t.Fatalf("想要 2 个候选（用户的 + 内置的），得到 %v", cands)
	}
	if cands[0] != filepath.Join(home, "somewhere", "new.json") {
		t.Errorf("用户候选没排在最前：%v", cands)
	}
	if cands[1] != filepath.Join(home, ".claude", "settings.json") {
		t.Errorf("内置候选没留作兜底：%v", cands)
	}
}

// Path 挑第一个真实存在的；一个都不存在时退回第一个候选，
// 这样报「不存在」的时候指向的是默认位置，而不是某个随手加的备选。
func TestPathPicksExistingElseFirst(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{"paths": {"claude.settings": ["~/nope.json", "~/yes.json"]}}`)
	k := Load(home)
	if got := k.Path("claude.settings"); got != filepath.Join(home, "nope.json") {
		t.Errorf("都不存在时应退回第一个候选，得到 %s", got)
	}
	real := filepath.Join(home, "yes.json")
	os.WriteFile(real, []byte("{}"), 0o600)
	if got := k.Path("claude.settings"); got != real {
		t.Errorf("应挑存在的那个，得到 %s", got)
	}
}

// 用户文件坏了必须报出来。静默退回内置知识是最坏的处理方式：
// 用户以为自己补的东西生效了，实际体检用的还是旧知识。
func TestBrokenUserFileIsReported(t *testing.T) {
	home := t.TempDir()
	write(t, home, `{ 这不是 JSON `)
	k := Load(home)
	if k.LoadErr == nil {
		t.Fatal("坏掉的用户文件被静默忽略了")
	}
	if k.Loaded {
		t.Error("没加载成功却报告加载了")
	}
	if _, ok := k.Provider("deepseek"); !ok {
		t.Error("回退到内置知识失败")
	}
}

func TestTemplateIsValidAndReloadable(t *testing.T) {
	home := t.TempDir()
	b, err := marshalTemplate()
	if err != nil {
		t.Fatal(err)
	}
	write(t, home, string(b))
	if k := Load(home); k.LoadErr != nil {
		t.Fatalf("--init 生成的模板自己都读不回来：%v", k.LoadErr)
	}
}

// OminiGate 一个域名两套协议，两条记录的 /v1 后缀必须相反：
// OpenAI 那套在 /v1，Anthropic 那套在根路径（Claude Code 自己补 /v1/messages，
// 给它带 /v1 的地址会 404 —— aivet 自己就有一条检查专门骂这件事）。
// 写反了不会报错，只会让所有 ominigate 用户探测失败，所以钉死。
func TestOminiGateProtocolsHaveOppositeV1Suffix(t *testing.T) {
	b := Builtin()
	chat, ok := b.Providers["ominigate"]
	if !ok {
		t.Fatal("内置里没有 ominigate")
	}
	if chat.Protocol != "chat" || !strings.HasSuffix(chat.BaseURL, "/v1") {
		t.Fatalf("OpenAI 那条要 chat 且以 /v1 结尾：%+v", chat)
	}
	ant, ok := b.Providers["ominigate-anthropic"]
	if !ok {
		t.Fatal("内置里没有 ominigate-anthropic")
	}
	if ant.Protocol != "anthropic" || strings.HasSuffix(ant.BaseURL, "/v1") {
		t.Fatalf("Anthropic 那条要 anthropic 且不带 /v1：%+v", ant)
	}
	for name, p := range map[string]Provider{"ominigate": chat, "ominigate-anthropic": ant} {
		if len(p.EnvKeys) == 0 || p.EnvKeys[0] != "OMINIGATE_API_KEY" {
			t.Fatalf("%s 的第一个 key 名要是 OMINIGATE_API_KEY：%+v", name, p.EnvKeys)
		}
	}
}
