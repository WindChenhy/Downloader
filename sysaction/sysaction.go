// Package sysaction 执行「全部下载结束」后的系统级动作。
// 动作取值与设置存储中的 afterComplete 字段对应；执行属应用层关注点，
// 引擎只产出设置字符串，不直接触碰操作系统。
package sysaction

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		return OpenFolder(openDir)
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

// OpenFolder 在系统文件管理器中打开目录。
func OpenFolder(dir string) error {
	switch runtime.GOOS {
	case "windows":
		return startCmd(exec.Command("explorer", dir))
	case "darwin":
		return startCmd(exec.Command("open", dir))
	default:
		return startCmd(exec.Command("xdg-open", dir))
	}
}

// RevealFile 在系统文件管理器中定位并选中指定文件。
// 目标不存在（或仍是目录）时退化为打开所在目录。
func RevealFile(dir, file string) error {
	target := filepath.Join(dir, file)
	if st, err := os.Stat(target); err == nil && !st.IsDir() {
		switch runtime.GOOS {
		case "windows":
			// explorer 接受 "/select,带空格路径" 整体作为一个参数的形式
			return startCmd(exec.Command("explorer", "/select,"+target))
		case "darwin":
			return startCmd(exec.Command("open", "-R", target))
		}
	}
	return OpenFolder(dir)
}

// startCmd 启动命令并回收子进程。
// 只 Start 不 Wait 的话，Unix 上子进程会一直挂成僵尸直到本进程退出；
// 但也不能同步 Wait —— pmset sleepnow 之类命令会阻塞到唤醒才返回，
// 所以放到后台 goroutine 里回收。
func startCmd(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// shutdownOS 关机；Windows/macOS/Linux 各自调用系统命令（Windows 延迟 60 秒）。
func shutdownOS() error {
	switch runtime.GOOS {
	case "windows":
		return startCmd(exec.Command("shutdown", "/s", "/t", "60"))
	case "darwin":
		return startCmd(exec.Command("osascript", "-e", `tell application "System Events" to shut down`))
	default:
		return startCmd(exec.Command("systemctl", "poweroff"))
	}
}

func sleepOS() error {
	switch runtime.GOOS {
	case "windows":
		return startCmd(exec.Command("rundll32.exe", "powrprof.dll,SetSuspendState", "0,1,0"))
	case "darwin":
		return startCmd(exec.Command("pmset", "sleepnow"))
	default:
		return startCmd(exec.Command("systemctl", "suspend"))
	}
}
