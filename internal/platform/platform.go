// Package platform 收纳操作系统差异：名字、shell、安装命令。
package platform

import (
	"os"
	"runtime"
	"strings"
)

// Label 返回给人看的系统名，如 "macOS arm64"、"Windows amd64"、"Linux (Ubuntu) amd64"。
func Label() string {
	name := map[string]string{"darwin": "macOS", "windows": "Windows", "linux": "Linux"}[runtime.GOOS]
	if name == "" {
		name = runtime.GOOS
	}
	if runtime.GOOS == "linux" {
		if d := linuxDistro(); d != "" {
			name += " (" + d + ")"
		}
		if os.Getenv("WSL_DISTRO_NAME") != "" {
			name += " · WSL"
		}
	}
	return name + " " + runtime.GOARCH
}

func linuxDistro() string {
	b, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return ""
}

// IsWindows 便捷判断。
func IsWindows() bool { return runtime.GOOS == "windows" }

// Install 返回某工具在当前系统上的安装命令（给 Hint 用）。
// 以各工具官方文档为准；这里只是把「第一步敲什么」放到用户眼前。
func Install(tool string) string {
	win := runtime.GOOS == "windows"
	switch tool {
	case "claude":
		if win {
			return `irm https://claude.ai/install.ps1 | iex   （或：npm install -g @anthropic-ai/claude-code）`
		}
		return `curl -fsSL https://claude.ai/install.sh | bash   （或：npm install -g @anthropic-ai/claude-code）`
	case "codex":
		if win {
			return `npm install -g @openai/codex`
		}
		return `npm install -g @openai/codex   （或：brew install codex）`
	case "hermes":
		if win {
			return `Hermes 目前建议在 WSL 里装：curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh | bash`
		}
		return `curl -fsSL https://raw.githubusercontent.com/NousResearch/hermes-agent/main/scripts/install.sh | bash`
	case "pi":
		return `npm install -g @mariozechner/pi-coding-agent`
	case "dsh":
		return `npm install -g @deepseek-ai/dsh`
	case "ccswitch":
		return `https://github.com/farion1231/cc-switch/releases 下载对应系统的安装包`
	}
	return ""
}

// NodeHint 说明「需要 Node.js」时该怎么装。
func NodeHint() string {
	switch runtime.GOOS {
	case "windows":
		return "装 Node.js：winget install OpenJS.NodeJS.LTS，或到 https://nodejs.org 下载 LTS"
	case "darwin":
		return "装 Node.js：brew install node，或到 https://nodejs.org 下载 LTS"
	default:
		return "装 Node.js：https://nodejs.org 下载 LTS，或用 nvm"
	}
}
