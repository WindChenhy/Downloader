// Package sysaction 执行「全部下载结束」后的系统级动作。
// 动作取值与设置存储中的 afterComplete 字段对应；执行属应用层关注点，
// 引擎只产出设置字符串，不直接触碰操作系统。
package sysaction

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
)

// 动作取值。
const (
	None     = "none"
	OpenDir  = "open_dir"
	Shutdown = "shutdown"
	Sleep    = "sleep"
	ExitApp  = "exit_app"
)

// ErrExitApp 信号：调用方收到后自行退出进程（本包不直接杀进程）。
var ErrExitApp = errors.New("exit_app")

// Run 执行动作；openDir 为空时 open_dir 退化为不打开。
func Run(action, openDir string) error {
	switch action {
	case None, "":
		return nil
	case OpenDir:
		if openDir == "" {
			return nil
		}
		return openFolder(openDir)
	case Shutdown:
		return shutdownOS()
	case Sleep:
		return sleepOS()
	case ExitApp:
		return ErrExitApp
	default:
		return fmt.Errorf("未知完成后动作: %s", action)
	}
}

func openFolder(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("explorer", dir).Start()
	case "darwin":
		return exec.Command("open", dir).Start()
	default:
		return exec.Command("xdg-open", dir).Start()
	}
}

// shutdownOS 关机；Windows/macOS/Linux 各自调用系统命令（Windows 延迟 60 秒）。
func shutdownOS() error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("shutdown", "/s", "/t", "60").Start()
	case "darwin":
		return exec.Command("osascript", "-e", `tell application "System Events" to shut down`).Start()
	default:
		return exec.Command("systemctl", "poweroff").Start()
	}
}

func sleepOS() error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0").Start()
	case "darwin":
		return exec.Command("pmset", "sleepnow").Start()
	default:
		return exec.Command("systemctl", "suspend").Start()
	}
}
