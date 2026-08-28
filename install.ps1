# aivet 安装脚本（Windows PowerShell）：下载单个 exe 到 %LOCALAPPDATA%\aivet 并加入用户 PATH。
#   irm https://raw.githubusercontent.com/shiftu/aivet/main/install.ps1 | iex
$ErrorActionPreference = "Stop"
$repo = "shiftu/aivet"
$arch = if ([System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture -eq "Arm64") { "arm64" } else { "amd64" }
$ver = $env:AIVET_VERSION
if (-not $ver) {
  $ver = (Invoke-RestMethod "https://api.github.com/repos/$repo/releases/latest").tag_name
}
$dir = Join-Path $env:LOCALAPPDATA "aivet"
New-Item -ItemType Directory -Force -Path $dir | Out-Null
$url = "https://github.com/$repo/releases/download/$ver/aivet_windows_$arch.exe"
Write-Host "下载 $url"
Invoke-WebRequest -Uri $url -OutFile (Join-Path $dir "aivet.exe")
$userPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($userPath -notlike "*$dir*") {
  [Environment]::SetEnvironmentVariable("Path", "$userPath;$dir", "User")
  $env:Path = "$env:Path;$dir"
  Write-Host "已把 $dir 加入用户 PATH（新开的终端生效）"
}
& (Join-Path $dir "aivet.exe") version

# 顺手把 Tab 补全装上。补全装不上不算安装失败，所以这里单独放行错误。
try { & (Join-Path $dir "aivet.exe") completion --install powershell } catch { Write-Host "补全没装上（不影响 aivet 本身）：$_" }

Write-Host "下一步：aivet        （体检）   aivet setup （新手向导）"
