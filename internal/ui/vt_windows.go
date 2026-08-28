//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// enableVT 打开 Windows 控制台的 ANSI 转义支持（Windows 10 1511+）。
func enableVT() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return
	}
	_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
}
