#!/usr/bin/env bash
# aivet 安装脚本（macOS / Linux）：下载对应平台的单个二进制到 ~/.local/bin。
#   curl -fsSL https://raw.githubusercontent.com/shiftu/aivet/main/install.sh | bash
# 可选环境变量：AIVET_VERSION=v0.1.0  AIVET_INSTALL_DIR=/usr/local/bin
set -euo pipefail

REPO="shiftu/aivet"
os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"
case "$os" in darwin|linux) ;; *) echo "不支持的系统：$os（Windows 请用 install.ps1）"; exit 1;; esac
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "不支持的架构：$arch"; exit 1 ;;
esac

ver="${AIVET_VERSION:-}"
if [ -z "$ver" ]; then
  ver="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -1)"
  [ -n "$ver" ] || { echo "拿不到最新版本号；可以手动指定 AIVET_VERSION=vX.Y.Z"; exit 1; }
fi

dir="${AIVET_INSTALL_DIR:-$HOME/.local/bin}"
mkdir -p "$dir"
url="https://github.com/${REPO}/releases/download/${ver}/aivet_${os}_${arch}"
echo "下载 $url"
tmp="$(mktemp)"
curl -fsSL -o "$tmp" "$url"
chmod +x "$tmp"
mv "$tmp" "$dir/aivet"
echo "已安装到 $dir/aivet"

case ":$PATH:" in
  *":$dir:"*) ;;
  *) echo; echo "⚠ $dir 不在 PATH 里。加一行到 ~/.zshrc 或 ~/.bashrc："; echo "    export PATH=\"$dir:\$PATH\"" ;;
esac
echo
"$dir/aivet" version

# 顺手把 Tab 补全装上 —— 这一步失败不该把整个安装拖下水（认不出 shell、rc 文件只读都可能），
# aivet 自己也会把这种情况当成「跳过」而不是错，这里再兜一层 || true。
"$dir/aivet" completion --install || true

echo "下一步：aivet        （体检）   aivet setup （新手向导）"
