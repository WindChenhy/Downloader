package engine

import (
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// runningTask 造一个正在下载的任务快照，供采样器直接驱动（不必等真实进度循环）。
func runningTask(id string, downloaded, speed int64) Task {
	return Task{
		ID: id, FileName: id + ".bin", URL: "http://example.com/" + id,
		Status: StatusRunning, Downloaded: downloaded, TotalSize: 100000,
		Speed: speed,
	}
}

func newTestStats(t *testing.T) *statsCollector {
	t.Helper()
	// 用例里的 flush 都用 force=true，不受 30s 落盘节流影响
	return newStatsCollector(filepath.Join(t.TempDir(), "stats.json"))
}

// TestStatsBytesAreExactDeltas 今日下载量取相邻快照的字节差值：
// 与采样间隔无关，不会因为「速度 × 时间」的窗口错位而多算或少算；
// 曲线值则取窗口平均速率，不受限速器整块放行配额的影响。
func TestStatsBytesAreExactDeltas(t *testing.T) {
	c := newTestStats(t)
	now := time.Now()

	c.sample([]Task{runningTask("a", 100, 50)}, now)                  // 首次只立基线
	c.sample([]Task{runningTask("a", 300, 80)}, now.Add(time.Second)) // 未到采样间隔：记字节不记点
	c.sample([]Task{runningTask("a", 700, 90)}, now.Add(2*time.Second))

	d := c.day(now)
	if d.Bytes != 700 {
		t.Fatalf("字节差值累加 = %d, want 700", d.Bytes)
	}
	// 窗口 [0, 2s) 内增量 600 字节 → 300 B/s
	if len(c.global) != 1 || c.global[0].V != 300 {
		t.Fatalf("曲线应按窗口平均速率记点: %v", c.global)
	}
	if d.PeakSpeed != 300 || d.Samples != 1 || d.activeAvg() != 300 {
		t.Fatalf("峰值/采样数/均值 = %+v", d)
	}
	ts := c.tasks["a"]
	if ts == nil || len(ts.Samples) != 1 || ts.Samples[0].V != 300 || ts.PeakSpeed != 300 {
		t.Fatalf("单任务曲线未跟上: %+v", ts)
	}
	if ts.FileName != "a.bin" {
		t.Fatalf("任务元数据未同步: %+v", ts)
	}

	// 完成后还有一截字节：另一个窗口均值；停下来先补一个 0 点让曲线落回基线，
	// 之后空闲不再攒无意义的 0
	done := []Task{{ID: "a", FileName: "a.bin", Status: StatusCompleted, Downloaded: 100000, TotalSize: 100000}}
	c.sample(done, now.Add(4*time.Second))
	c.sample(done, now.Add(6*time.Second))
	c.sample(done, now.Add(8*time.Second))
	// 窗口 [2s, 4s) 内增量 99300 → 49650 B/s
	if len(c.global) != 3 || c.global[1].V != 49650 || c.global[2].V != 0 {
		t.Fatalf("全局曲线 = %v, want [300, 49650, 0]", c.global)
	}
	if d := c.day(now); d.Bytes != 100000 || d.PeakSpeed != 49650 || d.Samples != 2 {
		t.Fatalf("完成后聚合不对: %+v", d)
	}
	// 任务已结束 → 不再给它补曲线点（只剩之前那一个）
	if len(c.tasks["a"].Samples) != 1 {
		t.Fatalf("已结束的任务不应再采新点: %v", c.tasks["a"].Samples)
	}
}

// TestStatsIdleWithoutCurve 全程没下载时不该产生任何曲线点，也不该把队列计数算成速度。
func TestStatsIdleWithoutCurve(t *testing.T) {
	c := newTestStats(t)
	now := time.Now()
	c.sample([]Task{{ID: "q", Status: StatusQueued}}, now)
	c.sample([]Task{{ID: "q", Status: StatusQueued}}, now.Add(3*time.Second))
	if len(c.global) != 0 {
		t.Fatalf("空闲不应记曲线点: %v", c.global)
	}
	if _, ok := c.tasks["q"]; ok {
		t.Fatal("一个字节都没下过的任务不该进任务汇总")
	}
}

// TestStatsRecordFinish 完成/失败按轮次计入当日；空间不足退回队列不算结束。
func TestStatsRecordFinish(t *testing.T) {
	c := newTestStats(t)
	now := time.Now()
	c.recordFinish(runningTask("a", 10, 0), StatusCompleted, now)
	c.recordFinish(runningTask("b", 10, 0), StatusFailed, now)
	c.recordFinish(runningTask("c", 10, 0), StatusQueued, now)
	d := c.day(now)
	if d.Completed != 1 || d.Failed != 1 {
		t.Fatalf("轮次计数 = %+v", d)
	}
}

// TestStatsPruneKeepsBounds 保留上限：过期日期与过多的任务汇总都要收掉。
func TestStatsPruneKeepsBounds(t *testing.T) {
	c := newTestStats(t)
	now := time.Now()
	c.daily[now.AddDate(0, 0, -statsDailyKeep-10).Format("2006-01-02")] = &DailyStat{Bytes: 1}
	c.daily[now.Format("2006-01-02")] = &DailyStat{Bytes: 2}
	for i := 0; i < statsSummaryKeep+30; i++ {
		id := fmt.Sprintf("t%03d", i)
		c.tasks[id] = &TaskStat{TaskID: id, UpdatedAt: now.Add(time.Duration(i)).UnixMilli()}
	}
	c.dirty = true // 直接摸数据造边界条件，靠 dirty 才能让 flush 真走一轮 prune
	if err := c.flush(now, true); err != nil {
		t.Fatal(err)
	}
	if len(c.daily) != 1 {
		t.Fatalf("过期日期应被收回: %d 条", len(c.daily))
	}
	if len(c.tasks) != statsSummaryKeep {
		t.Fatalf("任务汇总应裁到上限: %d 条", len(c.tasks))
	}
}

// TestStatsSeriesKeepBounds 落盘只给最近的几个任务留曲线，其余只留汇总：
// 体积上限靠这条约束，不是靠“全存下来再说”。
func TestStatsSeriesKeepBounds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "stats.json")
	c := newStatsCollector(path)
	now := time.Now()
	for i := 0; i < statsSeriesKeep+5; i++ {
		id := fmt.Sprintf("t%02d", i)
		c.tasks[id] = &TaskStat{
			TaskID:    id,
			UpdatedAt: now.Add(time.Duration(i) * time.Second).UnixMilli(),
			Samples:   []SpeedPoint{{T: now.UnixMilli(), V: 10}},
		}
	}
	c.dirty = true                             // flush 只写有变化的数据：直接摸 tasks 得自己置脏
	if err := c.flush(now, true); err != nil { // force：跳过 30s 节流
		t.Fatal(err)
	}

	c2 := newStatsCollector(path)
	if len(c2.tasks) != statsSeriesKeep+5 {
		t.Fatalf("汇总条数 = %d", len(c2.tasks))
	}
	var withSeries int
	for _, ts := range c2.sortedTasks(0) {
		if len(ts.Samples) > 0 {
			withSeries++
		}
	}
	if withSeries != statsSeriesKeep {
		t.Fatalf("保留曲线的任务数 = %d, want %d", withSeries, statsSeriesKeep)
	}
	// 留曲线的必须是最近更新的那几个
	if got := c2.sortedTasks(statsSeriesKeep)[0]; len(got.Samples) == 0 {
		t.Fatal("最近更新的任务应保留曲线")
	}
}

// TestStatsDropWithTaskRecord 任务记录被删时，曲线与差值基准一起消失（否则统计里
// 留着无主采样，长期挂机会一直涨）。
func TestStatsDropWithTaskRecord(t *testing.T) {
	c := newTestStats(t)
	now := time.Now()
	c.sample([]Task{runningTask("a", 500, 100)}, now)
	if len(c.tasks) != 1 || c.lastBytes["a"] != 500 {
		t.Fatalf("采样未记录: %+v", c)
	}
	c.drop("a")
	if len(c.tasks) != 0 || len(c.lastBytes) != 0 {
		t.Fatalf("删除后仍有残留: tasks=%v last=%v", c.tasks, c.lastBytes)
	}
	c.drop("不存在的 id") // 幂等
}

// TestStatsSurviveRestartAndDoNotDoubleCount 按天聚合跨重启保留，
// 且重启后首次采样不会把上次下好的字节再算一遍。
// 文件故意要下载几秒（每任务限速），否则整个任务在两个采样点之间就结束了，
// 曲线本来就没东西可画。
func TestStatsSurviveRestartAndDoNotDoubleCount(t *testing.T) {
	content := newContent(t, 60000)
	srv := httptest.NewServer(&testServer{content: content, etag: `"st"`})
	defer srv.Close()

	dataDir := t.TempDir()
	m, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m)
	saveDir := t.TempDir()
	task, err := m.AddTaskWithChecksum(AddTaskParams{
		URL: srv.URL, SaveDir: saveDir, Connections: 2, SpeedLimit: 20000,
	})
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return statusOf(m, task.ID) == StatusCompleted }, 25*time.Second)

	st := m.GetStats()
	if st.TodayBytes != 60000 {
		t.Fatalf("今日下载量 = %d, want 60000", st.TodayBytes)
	}
	if st.Completed != 1 || st.TodayCompleted != 1 || st.TaskCount != 1 {
		t.Fatalf("任务计数 = %+v", st)
	}
	if len(st.Global) < 2 {
		t.Fatalf("下载持续了几秒，全局曲线应有多个点: %v", st.Global)
	}
	if st.PeakSpeed <= 0 {
		t.Fatalf("峰值速度应大于 0: %+v", st)
	}
	var found *TaskStat
	for i := range st.Tasks {
		if st.Tasks[i].TaskID == task.ID {
			found = &st.Tasks[i]
		}
	}
	if found == nil || found.Bytes != 60000 || found.FileName == "" {
		t.Fatalf("任务汇总缺失或不对: %+v", st.Tasks)
	}
	// 汇总不再携带 Samples（避免每次轮询全量序列化），曲线按需经
	// GetTaskStats 拉取
	ts, ok := m.GetTaskStats(task.ID)
	if !ok || len(ts.Samples) == 0 {
		t.Fatal("运行期间应采到单任务曲线点")
	}

	if err := m.FlushStats(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(m.store.StatsPath()); err != nil {
		t.Fatalf("统计应已落盘: %v", err)
	}

	m2, err := NewManager(dataDir, nopNotify)
	if err != nil {
		t.Fatal(err)
	}
	stopOnCleanup(t, m2)
	st2 := m2.GetStats()
	if st2.TodayBytes != 60000 {
		t.Fatalf("重启后今日下载量应保留: %d", st2.TodayBytes)
	}
	// 再等几个真实 tick：差值基准已从落盘进度起算，不该重复计数
	time.Sleep(1200 * time.Millisecond)
	if got := m2.GetStats().TodayBytes; got != 60000 {
		t.Fatalf("重启后重复计数今日下载量: %d", got)
	}
	// 单任务曲线也随落盘带回来（最近的几个任务），重启后统计页不是空的。
	// 汇总不再携带 Samples，按需经 GetTaskStats 拉取
	if len(st2.Tasks) != 1 {
		t.Fatalf("重启后应保留最近任务汇总: %+v", st2.Tasks)
	}
	ts2, ok := m2.GetTaskStats(task.ID)
	if !ok || len(ts2.Samples) == 0 {
		t.Fatalf("重启后应保留最近任务的曲线: %+v", ts2)
	}
}

// TestStatsTotalAndWeekWindow 累计与近 7 天窗口按天聚合取值，不含未来日期。
func TestStatsTotalAndWeekWindow(t *testing.T) {
	c := newTestStats(t)
	now := time.Now()
	day := func(offset int) string { return now.AddDate(0, 0, offset).Format("2006-01-02") }
	c.daily[day(0)] = &DailyStat{Date: day(0), Bytes: 100, Completed: 1}
	c.daily[day(-3)] = &DailyStat{Date: day(-3), Bytes: 200}
	c.daily[day(-30)] = &DailyStat{Date: day(-30), Bytes: 400}
	c.global = []SpeedPoint{{T: now.UnixMilli(), V: 10}, {T: now.UnixMilli(), V: 30}}

	s := c.snapshot(now, nil)
	if s.TodayBytes != 100 || s.WeekBytes != 300 || s.TotalBytes != 700 {
		t.Fatalf("窗口取值 = %+v", s)
	}
	if s.TodayCompleted != 1 || s.PeakSpeed != 30 || s.AvgSpeed != 20 {
		t.Fatalf("当日计数与曲线口径 = %+v", s)
	}
	if len(s.Daily) != 3 || s.Daily[0].Date > s.Daily[2].Date {
		t.Fatalf("按天列表应升序: %+v", s.Daily)
	}
}
