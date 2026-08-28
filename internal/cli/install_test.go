package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 每家 shell 都得能真装上，否则 install.sh 里那句「顺手装好补全」就是空话。
func TestInstallWritesScriptAndRC(t *testing.T) {
	for _, sh := range Shells {
		t.Run(sh, func(t *testing.T) {
			home := t.TempDir()
			ps := filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
			res, err := Install(sh, home, ps)
			if err != nil {
				t.Fatalf("装 %s 失败：%v", sh, err)
			}
			if !res.ScriptWritten {
				t.Errorf("%s 第一次装却说没写脚本", sh)
			}
			b, err := os.ReadFile(res.ScriptPath)
			if err != nil {
				t.Fatalf("%s 的脚本没落盘：%v", sh, err)
			}
			want, _ := Script(sh)
			if string(b) != want {
				t.Errorf("%s 落盘的内容和 Script() 给的不是同一份", sh)
			}
			if !strings.HasPrefix(res.ScriptPath, home) {
				t.Errorf("%s 把脚本写到了家目录外面：%s", sh, res.ScriptPath)
			}
			if sh == "fish" {
				if res.RCPath != "" {
					t.Error("fish 会自动加载 completions 目录，不该动配置文件")
				}
				return
			}
			rc, err := os.ReadFile(res.RCPath)
			if err != nil {
				t.Fatalf("%s 的 rc 没写：%v", sh, err)
			}
			if !strings.Contains(string(rc), rcBegin) || !strings.Contains(string(rc), rcEnd) {
				t.Errorf("%s 往 rc 里写的东西没带首尾标记，重装时删不掉", sh)
			}
		})
	}
	if _, err := Install("tcsh", t.TempDir(), ""); err == nil {
		t.Error("不认识的 shell 该报错")
	}
}

// 重装是常事（每次 install.sh 都会跑一遍），rc 里不能越叠越多。
func TestInstallIsIdempotent(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("# 我自己写的\nexport FOO=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		res, err := Install("zsh", home, "")
		if err != nil {
			t.Fatalf("第 %d 次：%v", i+1, err)
		}
		if i > 0 && (res.ScriptWritten || res.RCWritten) {
			t.Errorf("第 %d 次装什么都没变，却报告说写了", i+1)
		}
	}
	b, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), rcBegin); n != 1 {
		t.Errorf("装了 3 次，rc 里有 %d 段 aivet 的块，应该只有 1 段", n)
	}
	if !strings.Contains(string(b), "export FOO=1") {
		t.Error("把用户原来写的东西弄丢了")
	}
}

// 升级换了 rc 里那几行的写法时，得把旧块换掉，而不是留着旧的再加一份新的。
func TestInstallReplacesStaleBlock(t *testing.T) {
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	stale := "export FOO=1\n" + rcBegin + "\nsource ~/上个版本的写法\n" + rcEnd + "\nexport BAR=2\n"
	if err := os.WriteFile(rc, []byte(stale), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Install("zsh", home, ""); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if strings.Contains(got, "上个版本的写法") {
		t.Error("旧块没被换掉")
	}
	if strings.Count(got, rcBegin) != 1 {
		t.Error("换块变成了追加")
	}
	if !strings.Contains(got, "export FOO=1") || !strings.Contains(got, "export BAR=2") {
		t.Error("块前后用户自己的行被吃掉了")
	}
}

// zsh 那几行是加在 .zshrc 末尾的，也就是 oh-my-zsh 跑完 compinit 之后。
// 少了这个守卫，没开过补全的 .zshrc 会直接报 compdef: command not found。
func TestZshRCLinesSurviveBothOrders(t *testing.T) {
	plan, ok := Plan("zsh", "/home/u", "")
	if !ok {
		t.Fatal("zsh 该有安装方案")
	}
	joined := strings.Join(plan.RCLines, "\n")
	if !strings.Contains(joined, "compinit") {
		t.Error("没兜住「这个 .zshrc 从没跑过 compinit」的情况")
	}
	if !strings.Contains(joined, "fpath") {
		t.Error("没把补全目录加进 fpath")
	}
	if !strings.Contains(joined, "source") {
		t.Error("compinit 已经跑过时只能靠 source + 脚本自带的双模守卫，这行不能少")
	}
}

// PowerShell 的落点得跟着 $PROFILE 走：OneDrive 会把「文档」整个重定向，自己拼路径会拼错地方。
func TestPowerShellFollowsProfilePath(t *testing.T) {
	profile := filepath.Join("/x", "OneDrive", "文档", "PowerShell", "Microsoft.PowerShell_profile.ps1")
	plan, ok := Plan("powershell", "/home/u", profile)
	if !ok {
		t.Fatal("powershell 该有安装方案")
	}
	if plan.RCPath != profile {
		t.Errorf("要改的是 $PROFILE 本人，got %s", plan.RCPath)
	}
	if filepath.Dir(plan.ScriptPath) != filepath.Dir(profile) {
		t.Errorf("脚本该放在 $PROFILE 旁边，got %s", plan.ScriptPath)
	}
	if _, ok := Plan("powershell", "/home/u", ""); ok {
		t.Error("问不到 $PROFILE 就不该硬装")
	}
}
