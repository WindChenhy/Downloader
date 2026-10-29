package sysaction

import (
	"errors"
	"testing"
)

// 只覆盖「不会真的执行外部命令」的分支：none / 空动作 / 未知动作 /
// 空目录的 open_dir / exit_app 信号。关机、睡眠、真正打开文件管理器
// 会调用系统命令，不在单测里触发。

// TestRunDispatchWithoutSideEffects 动作分发的边界分支。
func TestRunDispatchWithoutSideEffects(t *testing.T) {
	if err := Run(None, "/tmp"); err != nil {
		t.Fatalf("none 不应报错: %v", err)
	}
	if err := Run("", "/tmp"); err != nil {
		t.Fatalf("空动作应等价 none: %v", err)
	}
	// open_dir 但没有目录可开 → 静默跳过，不启动文件管理器
	if err := Run(OpenDir, ""); err != nil {
		t.Fatalf("空目录的 open_dir 应静默跳过: %v", err)
	}

	// 未知动作要报错，且不能与 ErrExitApp 混淆（调用方据此决定是否退出进程）
	err := Run("who-knows", "")
	if err == nil {
		t.Fatal("未知动作应报错")
	}
	if errors.Is(err, ErrExitApp) {
		t.Fatalf("未知动作不该返回 ErrExitApp: %v", err)
	}

	// exit_app 不直接杀进程，只返回哨兵错误交给调用方
	if err := Run(ExitApp, ""); !errors.Is(err, ErrExitApp) {
		t.Fatalf("exit_app 应返回 ErrExitApp, got %v", err)
	}
}

// TestActionConstantsAreUnique 动作取值会持久化进 settings.json，
// 重复即串台；同时不能与 none 的空串语义撞车。
func TestActionConstantsAreUnique(t *testing.T) {
	seen := map[string]bool{}
	for _, v := range []string{None, OpenDir, Shutdown, Sleep, ExitApp} {
		if v == "" {
			t.Fatal("动作取值不应为空字符串")
		}
		if seen[v] {
			t.Fatalf("动作取值重复: %q", v)
		}
		seen[v] = true
	}
}
