package engine

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// 带宽时间窗：只在允许的时段内启动新任务（如夜间闲时下载）。
// 窗外 = 自动闭闸，正在下载的任务不受影响、跑完那一轮就停在队列里，
// 与用户手动的队列总闸共用同一条 dispatch 判定，不引入第二套停摆语义。

// parseHHMM 解析 "HH:MM"（24 小时制）为当日分钟数。
func parseHHMM(v string) (int, bool) {
	h, mm, ok := strings.Cut(strings.TrimSpace(v), ":")
	if !ok {
		return 0, false
	}
	hi, err1 := strconv.Atoi(strings.TrimSpace(h))
	mi, err2 := strconv.Atoi(strings.TrimSpace(mm))
	if err1 != nil || err2 != nil || hi < 0 || hi > 23 || mi < 0 || mi > 59 {
		return 0, false
	}
	return hi*60 + mi, true
}

// hhmm 把分钟数格式化回 "HH:MM"。
func hhmm(minute int) string {
	return fmt.Sprintf("%02d:%02d", minute/60, minute%60)
}

// windowContains 判断 now 是否落在 [start, end) 时段内（本地时钟，按分钟粒度）。
// start > end 表示跨零点（如 22:00-06:00）；start == end 或任一项非法都视为全天放行——
// 时间窗是省带宽的便利项，不该因为一个填错的表单把队列永久锁死。
func windowContains(start, end string, now time.Time) bool {
	s, ok1 := parseHHMM(start)
	e, ok2 := parseHHMM(end)
	if !ok1 || !ok2 || s == e {
		return true
	}
	cur := now.Hour()*60 + now.Minute()
	if s < e {
		return cur >= s && cur < e
	}
	return cur >= s || cur < e
}

// windowOpensAt 返回下一个窗内时刻（供界面提示「08:00 自动放行」）。
// 已在窗内或未启用时返回 false。
func windowOpensAt(start, end string, now time.Time) (time.Time, bool) {
	s, ok1 := parseHHMM(start)
	e, ok2 := parseHHMM(end)
	if !ok1 || !ok2 || s == e || windowContains(start, end, now) {
		return time.Time{}, false
	}
	cur := now.Hour()*60 + now.Minute()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	// 今天的开始点已过（跨零点窗口的右半边）→ 明天再开
	if cur >= s {
		return day.Add(24 * time.Hour).Add(time.Duration(s) * time.Minute), true
	}
	return day.Add(time.Duration(s) * time.Minute), true
}

// QueueStatus 队列放行状态，供界面横幅与 REST 读取。
// 闸门、时间窗与「写不下」三件事分开报，界面才能说清楚任务为什么没在动。
type QueueStatus struct {
	Active        bool   `json:"active"`        // 此刻允许启动新任务
	Suspended     bool   `json:"suspended"`     // 用户总闸（QueueSuspended）
	WindowEnabled bool   `json:"windowEnabled"` // 启用了带宽时间窗
	WindowActive  bool   `json:"windowActive"`  // 当前时刻在窗内
	WindowStart   string `json:"windowStart"`
	WindowEnd     string `json:"windowEnd"`
	OpensAt       string `json:"opensAt"`      // 窗外时下一次放行的时刻（RFC3339）
	BlockedTasks  int    `json:"blockedTasks"` // 因磁盘空间不足停在队列里的任务数
}
