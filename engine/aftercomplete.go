package engine

import (
	"fmt"
	"os/exec"
	"runtime"
)

// RunAfterCompleteAction 执行「全部下载结束」动作。
// openDir 为空时 open_dir 动作退化为不打开。
// AfterCompleteExitApp 不在此处退出进程，而是返回 errExitApp 交 App 处理。
func RunAfterCompleteAction(action, openDir string) error {
	switch action {
	case AfterCompleteNone, "":
		return nil
	case AfterCompleteOpenDir:
		if openDir == "" {
			return nil
		}
		return openFolder(openDir)
	case AfterCompleteShutdown:
		return shutdownOS()
	case AfterCompleteSleep:
		return sleepOS()
	case AfterCompleteExitApp:
		return errExitApp
	default:
		return fmt.Errorf("未知完成后动作: %s", action)
	}
}

// errExitApp 信号：App 收到后调用 requestQuit。
var errExitApp = fmt.Errorf("exit_app")

// IsExitAppAction 判断错误是否为退出应用信号。
func IsExitAppAction(err error) bool { return err == errExitApp }

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
