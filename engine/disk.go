package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// 磁盘空间预检查：任务在真正起跑前确认「写得下」，避免下到几个 G 才发现盘满。
// 被拦下的任务不改状态——停在 queued 并写明原因，由 progressLoop 的 dispatch
// 复用同一条路径自动复检，空间腾出来就自己开始。

// spaceProbeTTL 卷剩余空间的缓存时长。progressLoop 每 500ms 调一次
// dispatchLocked，每个候选任务都做 syscall 既没必要也会被慢盘拖住，
// 这里按卷缓存；同时也界定了「复检」的最小间隔。
const spaceProbeTTL = 2 * time.Second

// spaceCheck 一处落盘需求的描述。
type spaceCheck struct {
	dir  string
	need int64 // 该卷至少要腾出多少字节
}

// volumeKey 归一到卷标识：Windows 取盘符根（D:\），其余平台取最近的既存祖先目录
// （statfs 沿路径能解析到挂载点；暂存目录还没建出来时也能查）。
// 返回值一定存在于磁盘，可直接交给系统调用。
func volumeKey(dir string) string {
	if v := filepath.VolumeName(dir); v != "" {
		return strings.ToUpper(v) + string(filepath.Separator)
	}
	for d := filepath.Clean(dir); ; {
		if _, err := os.Stat(d); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return d
		}
		d = parent
	}
}

// fileNeed 返回任务本轮还要写入的字节数；大小未知时返回 -1（跳过按量检查）。
func fileNeed(t Task) int64 {
	if t.TotalSize <= 0 {
		return -1
	}
	need := t.TotalSize - t.Downloaded
	if need < 0 {
		need = 0
	}
	return need
}

// spaceChecks 列出任务本轮的落盘需求位置。
// 数据先进统一暂存目录，完成时才移动/复制到保存目录：
//   - 暂存卷需要「剩余未下载量」；
//   - 跨卷时保存卷需要「整个文件大小」（复制发生在下载完成之后）。
//
// 同一卷只查一次，避免重复统计。
func (m *Manager) spaceChecksLocked(t Task) []spaceCheck {
	need := fileNeed(t)
	staging := m.stagingDir()
	checks := []spaceCheck{{dir: staging, need: need}}
	if volumeKey(t.SaveDir) != volumeKey(staging) {
		total := t.TotalSize
		if total <= 0 {
			total = -1 // 大小未知：只按余量阈值检查
		}
		checks = append(checks, spaceCheck{dir: t.SaveDir, need: total})
	}
	return checks
}

// cachedFreeLocked 返回 dir 所在卷的剩余字节，按卷缓存 spaceProbeTTL。
// 调用方须持有 mu。
func (m *Manager) cachedFreeLocked(dir string, now time.Time) (int64, error) {
	key := volumeKey(dir)
	if p, ok := m.spaceCache[key]; ok && now.Sub(p.at) < spaceProbeTTL {
		return p.free, p.err
	}
	free, err := m.freeSpace(key)
	m.spaceCache[key] = &spaceStat{at: now, free: free, err: err}
	return free, err
}

// spaceBlockLocked 返回任务因磁盘空间不足应被拦下的原因，空串表示可以启动。
// 查询失败（卷不存在、权限不足）一律放行：预检查是保险丝，不该变成新的失败源。
// 调用方须持有 mu。
func (m *Manager) spaceBlockLocked(t Task, now time.Time) string {
	if m.settings.DiskGuardOff {
		return ""
	}
	margin := m.settings.DiskMinFree
	if margin < 0 {
		margin = 0
	}
	for _, c := range m.spaceChecksLocked(t) {
		free, err := m.cachedFreeLocked(c.dir, now)
		if err != nil {
			continue
		}
		// 先扣掉要为用户保留的余量，再和需求量比较：两侧都不会溢出
		avail := free - margin
		if avail < 0 {
			avail = 0
		}
		need := c.need
		if need < 0 {
			need = 0 // 未知大小只要求保住余量
		}
		if need > avail {
			msg := fmt.Sprintf("磁盘空间不足：%s 剩余 %s，本任务还需 %s",
				volumeKey(c.dir), humanBytes(free), humanBytes(need))
			if margin > 0 {
				msg += fmt.Sprintf("（另预留 %s）", humanBytes(margin))
			}
			return msg
		}
	}
	return ""
}

// spaceStat 一次卷剩余空间的采样结果。
type spaceStat struct {
	at   time.Time
	free int64
	err  error
}

// spaceReasonFor 用已知的文件大小再做一次磁盘空间检查，返回拦下的原因（空 = 写得下）。
// taskRunner 探测到真实大小后调它：dispatch 那一次对新任务只能按「大小未知」处理。
func (m *Manager) spaceReasonFor(t Task, total int64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if total > 0 {
		t.TotalSize = total
	}
	return m.spaceBlockLocked(t, m.now())
}

// noSpaceError 带具体原因的空间错误：界面要能说出「剩多少、还差多少」，
// 同时保留 errors.Is(err, ErrNoSpace) 让收尾逻辑区分“退回队列”与“失败”。
type noSpaceError struct {
	reason string
}

func (e *noSpaceError) Error() string { return e.reason }

func (e *noSpaceError) Unwrap() error { return ErrNoSpace }

// humanBytes 提示用的人类可读字节数（1024 进制，与界面口径一致）。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
