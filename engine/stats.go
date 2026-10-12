package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

// 下载统计与速度曲线。
//
// 字节数不做「速度 × 时间」的积分：进度循环每次已经在结算各任务的已下载字节，
// 累加相邻两次快照的差值就是精确字节，不会因为采样窗口与速度结算窗口不一致而漂移。
// 曲线点则按固定间隔抽稀（statsSampleEvery），字节差值每个 tick 都记。
//
// 落盘范围：按天聚合、最近一段全局曲线、任务汇总，外加最近 statsSeriesKeep 个
// 任务自己的曲线。曲线不能存太多：一个任务 240 点，给几十个任务全存下，
// stats.json 能涨到几 MB 且每次写盘都要重写这些点；只留最近的十几个任务（约
// 100KB），重启后统计页仍有东西可看，体积有上限。

const (
	statsSampleEvery  = 2 * time.Second  // 曲线采样间隔
	statsFlushEvery   = 30 * time.Second // 落盘节流
	statsGlobalPoints = 900              // 全局曲线保留点数（≈30 分钟）
	statsTaskPoints   = 240              // 单任务曲线保留点数（≈8 分钟）
	statsDailyKeep    = 90               // 按天聚合保留天数
	statsSummaryKeep  = 100              // 任务汇总保留条数
	statsSeriesKeep   = 15               // 落盘保留曲线的任务数（按最近更新取前 N 个）
	statsPayloadTasks = 40               // 一次 GetStats 最多带回的任务汇总
	statsVersion      = 2
)

// SpeedPoint 速度采样点：T 为毫秒时间戳，V 为字节/秒。
type SpeedPoint struct {
	T int64 `json:"t"`
	V int64 `json:"v"`
}

// DailyStat 单日聚合。Completed/Failed 记的是「结束轮次」：一个任务失败后重试
// 并成功，会在 failed 里留一次、completed 里留一次，与用户的直观感受一致。
type DailyStat struct {
	Date      string `json:"date"` // 2006-01-02（本地日）
	Bytes     int64  `json:"bytes"`
	Completed int    `json:"completed"`
	Failed    int    `json:"failed"`
	PeakSpeed int64  `json:"peakSpeed"` // 当日聚合速度峰值
	SumSpeed  int64  `json:"sumSpeed"`  // 活跃采样点的速度合计
	Samples   int64  `json:"samples"`
}

// activeAvg 有下载在跑时的平均聚合速度。
func (d DailyStat) activeAvg() int64 {
	if d.Samples <= 0 {
		return 0
	}
	return d.SumSpeed / d.Samples
}

// TaskStat 单任务统计。Samples 只给最近的几个任务落盘（statsSeriesKeep），
// 其余任务只留汇总。
type TaskStat struct {
	TaskID    string       `json:"taskId"`
	FileName  string       `json:"fileName"`
	URL       string       `json:"url"`
	Status    Status       `json:"status"`
	Bytes     int64        `json:"bytes"`
	TotalSize int64        `json:"totalSize"`
	AvgSpeed  int64        `json:"avgSpeed"`
	PeakSpeed int64        `json:"peakSpeed"`
	ActiveMs  int64        `json:"activeMs"`
	UpdatedAt int64        `json:"updatedAt"`
	Samples   []SpeedPoint `json:"samples,omitempty"`
}

// Stats 统计页一次读取的全部数据。PeakSpeed/AvgSpeed/CurrentSpeed 都只覆盖
// 内存里那段全局曲线（约 30 分钟），界面上要标明口径，别让人以为是历史均值。
type Stats struct {
	GeneratedAt    int64        `json:"generatedAt"`
	TodayBytes     int64        `json:"todayBytes"`
	WeekBytes      int64        `json:"weekBytes"`
	TotalBytes     int64        `json:"totalBytes"`
	CurrentSpeed   int64        `json:"currentSpeed"` // 此刻在跑任务的速度合计
	PeakSpeed      int64        `json:"peakSpeed"`    // 曲线窗口内峰值
	AvgSpeed       int64        `json:"avgSpeed"`     // 曲线窗口内活跃均值
	WindowMs       int64        `json:"windowMs"`     // 曲线窗口跨度
	GlobalFrom     int64        `json:"globalFrom"`   // 曲线起点（毫秒时间戳）
	GlobalTo       int64        `json:"globalTo"`     // 曲线终点
	TodayCompleted int          `json:"todayCompleted"`
	TodayFailed    int          `json:"todayFailed"`
	TaskCount      int          `json:"taskCount"`
	Running        int          `json:"running"`
	Queued         int          `json:"queued"`
	Completed      int          `json:"completed"`
	Failed         int          `json:"failed"`
	Paused         int          `json:"paused"`
	Daily          []DailyStat  `json:"daily"`
	Global         []SpeedPoint `json:"global"`
	Tasks          []TaskStat   `json:"tasks"`
}

// statsPersisted stats.json 的结构。
type statsPersisted struct {
	Version int          `json:"version"`
	Daily   []DailyStat  `json:"daily"`
	Global  []SpeedPoint `json:"global"`
	Tasks   []TaskStat   `json:"tasks"`
}

// statsCollector 统计的累加与落盘。所有方法都要求调用方持有 Manager.mu，
// 采样本来就发生在进度循环的临界区里，不再单独加锁。
type statsCollector struct {
	path        string
	daily       map[string]*DailyStat
	global      []SpeedPoint
	tasks       map[string]*TaskStat
	lastBytes   map[string]int64 // 上次快照的已下载字节，用于求精确增量
	windowBytes int64            // 本采样窗口内的字节增量
	windowTask  map[string]int64 // 本窗口内各任务的字节增量
	lastSample  time.Time
	lastFlush   time.Time
	dirty       bool
}

// newStatsCollector 读盘并以当前真实时间立落盘基准：刚启动就写盘对短生命周期的
// 进程没意义，首批采样等到一个完整节流周期后再落。
func newStatsCollector(path string) *statsCollector {
	c := &statsCollector{
		path:       path,
		daily:      map[string]*DailyStat{},
		tasks:      map[string]*TaskStat{},
		lastBytes:  map[string]int64{},
		windowTask: map[string]int64{},
		lastFlush:  time.Now(),
	}
	var p statsPersisted
	if err := readJSON(c.path, &p); err == nil {
		for i := range p.Daily {
			d := p.Daily[i]
			c.daily[d.Date] = &d
		}
		c.global = tailPoints(p.Global, statsGlobalPoints)
		for i := range p.Tasks {
			t := p.Tasks[i]
			if t.TaskID != "" {
				// 曲线随文件带回来：重启后这个任务再看曲线不是空的，
				// 续传任务也能接着旧曲线往右画
				if len(t.Samples) > statsTaskPoints {
					t.Samples = tailPoints(t.Samples, statsTaskPoints)
				}
				c.tasks[t.TaskID] = &t
			}
		}
	}
	// 文件缺失或损坏都从空统计开始：统计是观察数据，不该挡住程序启动
	return c
}

// tailPoints 取末尾 n 个采样点（丢弃更早的）。
func tailPoints(in []SpeedPoint, n int) []SpeedPoint {
	if len(in) <= n {
		return append([]SpeedPoint(nil), in...)
	}
	return append([]SpeedPoint(nil), in[len(in)-n:]...)
}

// day 取（必要时创建）某天的聚合槽位。
func (c *statsCollector) day(now time.Time) *DailyStat {
	key := now.Format("2006-01-02")
	if d, ok := c.daily[key]; ok {
		return d
	}
	d := &DailyStat{Date: key}
	c.daily[key] = d
	return d
}

// windowRate 采样窗口内的平均速率（字节/秒）。
func windowRate(bytes int64, elapsed time.Duration) int64 {
	if bytes <= 0 || elapsed <= 0 {
		return 0
	}
	return int64(float64(bytes) / elapsed.Seconds())
}

// sample 用一次任务快照更新统计；tasks 来自 snapshotLocked 或进度循环。
//
// 曲线值取「窗口内的字节增量 / 窗口时长」，不是某一 tick 的瞬时速度：
// 限速器按 32KB 整块放行配额，瞬时速度是锯齿形的，而采样间隔（2s）正好是
// 进度循环间隔（500ms）的整数倍，直接拿瞬时值会稳定地采到 0（实测曲线一片空白）。
func (c *statsCollector) sample(tasks []Task, now time.Time) {
	d := c.day(now)
	for _, t := range tasks {
		last := c.lastBytes[t.ID]
		if delta := t.Downloaded - last; delta > 0 {
			d.Bytes += delta
			c.windowBytes += delta
			if c.windowTask == nil {
				c.windowTask = map[string]int64{}
			}
			c.windowTask[t.ID] += delta
			c.dirty = true
		}
		if t.Downloaded != last {
			c.lastBytes[t.ID] = t.Downloaded
		}
		c.track(t, now)
	}

	if c.lastSample.IsZero() {
		c.lastSample = now // 首次只立基线：从零时间算出的窗口没有意义
		c.windowBytes = 0
		c.windowTask = map[string]int64{}
		return
	}
	elapsed := now.Sub(c.lastSample)
	if elapsed < statsSampleEvery {
		return // 字节差值已逐 tick 累加，曲线点按间隔抽稀
	}
	c.lastSample = now

	rates := make(map[string]int64, len(c.windowTask))
	for id, b := range c.windowTask {
		if r := windowRate(b, elapsed); r > 0 {
			rates[id] = r
		}
	}
	var aggregate int64
	for _, r := range rates {
		aggregate += r
	}
	c.windowBytes = 0
	c.windowTask = map[string]int64{}

	// 完全空闲时不再补 0 点：上一条已经是 0 就说明曲线已经落回基线，
	// 挂机一整夜会攒出几千个无意义的点。
	idle := aggregate <= 0 && (len(c.global) == 0 || c.global[len(c.global)-1].V == 0)
	if idle {
		return
	}
	c.global = append(c.global, SpeedPoint{T: now.UnixMilli(), V: aggregate})
	if len(c.global) > statsGlobalPoints {
		c.global = append(c.global[:0], c.global[len(c.global)-statsGlobalPoints:]...)
	}
	d.PeakSpeed = max(d.PeakSpeed, aggregate)
	if aggregate > 0 {
		d.SumSpeed += aggregate
		d.Samples++
	}
	c.dirty = true

	stamp := now.UnixMilli()
	for _, t := range tasks {
		if t.Status != StatusRunning {
			continue
		}
		ts := c.tasks[t.ID]
		if ts == nil {
			continue
		}
		// 包络中没这个任务（这窗口零字节，但仍在跑）时补 0，曲线才不会断线
		rate := rates[t.ID]
		ts.Samples = append(ts.Samples, SpeedPoint{T: stamp, V: rate})
		if len(ts.Samples) > statsTaskPoints {
			ts.Samples = append(ts.Samples[:0], ts.Samples[len(ts.Samples)-statsTaskPoints:]...)
		}
		if rate > ts.PeakSpeed {
			ts.PeakSpeed = rate
		}
	}
}

// track 建立或刷新单任务汇总（元数据每次都同步，曲线由 sample 统一节奏）。
func (c *statsCollector) track(t Task, now time.Time) {
	ts := c.tasks[t.ID]
	if ts == nil {
		// 一个字节都没下过的历史任务不建条目，免得统计里塞满空壳
		if t.Downloaded <= 0 && t.Status != StatusRunning {
			return
		}
		ts = &TaskStat{TaskID: t.ID}
		c.tasks[t.ID] = ts
	}
	ts.FileName = t.FileName
	ts.URL = t.URL
	ts.Status = t.Status
	ts.TotalSize = t.TotalSize
	ts.Bytes = t.Downloaded
	ts.AvgSpeed = t.AvgSpeed
	ts.ActiveMs = t.ActiveMs
	ts.UpdatedAt = now.UnixMilli()
	// 峰值不在这里算：瞬时速度受限速器整块放行影响，与曲线同口径地由窗口速率算
}

// recordFinish 记一次轮次结束（完成 / 失败）。空间不足退回队列不算结束，
// 由调用方（finishTask）分状态调用。
func (c *statsCollector) recordFinish(t Task, status Status, now time.Time) {
	switch status {
	case StatusCompleted:
		c.day(now).Completed++
	case StatusFailed:
		c.day(now).Failed++
	default:
		return
	}
	c.track(t, now)
	c.dirty = true
}

// drop 任务记录被删除时，统计侧的同名条目与差值基准一起清掉，避免无界增长。
func (c *statsCollector) drop(id string) {
	if _, ok := c.tasks[id]; !ok {
		if _, ok := c.lastBytes[id]; !ok {
			return
		}
	}
	delete(c.tasks, id)
	delete(c.lastBytes, id)
	c.dirty = true
}

// sortedTasks 按最近更新排序，供落盘与快照共用。
func (c *statsCollector) sortedTasks(limit int) []TaskStat {
	list := make([]TaskStat, 0, len(c.tasks))
	for _, ts := range c.tasks {
		list = append(list, *ts)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].UpdatedAt > list[j].UpdatedAt })
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return list
}

// prune 收回保留上限：过期日期与过多的任务汇总。
func (c *statsCollector) prune(now time.Time) {
	oldest := now.AddDate(0, 0, -statsDailyKeep).Format("2006-01-02")
	for key := range c.daily {
		if key < oldest {
			delete(c.daily, key)
		}
	}
	if len(c.tasks) <= statsSummaryKeep {
		return
	}
	// 任务汇总被裁掉时，内存里的曲线与差值基准一起走（否则曲线无界增长）
	keep := map[string]bool{}
	for _, ts := range c.sortedTasks(statsSummaryKeep) {
		keep[ts.TaskID] = true
	}
	for id := range c.tasks {
		if !keep[id] {
			delete(c.tasks, id)
			delete(c.lastBytes, id)
		}
	}
}

// payload 组装待落盘的字节（持锁调用）：裁剪、序列化、清 dirty 与节流判定
// 都在这一步完成。写盘不在这里做——持锁写会阻塞进度循环与所有 Manager 操作，
// 违反「写盘不持锁」的既定约定，由调用方拿到字节后在锁外落盘
// （见 Manager.writeStatsData / progressLoop / finishTask）。
// 节流窗口用真实单调时间（time.Since）而不是业务时钟：系统时间被改（或测试
// 里的假时钟跳两小时）不该把写盘节流解开造成写风暴；日期归集仍用业务时钟。
// 返回 ok=false 表示本轮无需写（无变化、未到节流或序列化失败）。
func (c *statsCollector) payload(now time.Time, force bool) ([]byte, bool) {
	if !c.dirty {
		return nil, false
	}
	if !force && !c.lastFlush.IsZero() && time.Since(c.lastFlush) < statsFlushEvery {
		return nil, false
	}
	c.prune(now)
	p := statsPersisted{Version: statsVersion, Global: append([]SpeedPoint(nil), c.global...)}
	for _, d := range c.daily {
		p.Daily = append(p.Daily, *d)
	}
	sort.Slice(p.Daily, func(i, j int) bool { return p.Daily[i].Date < p.Daily[j].Date })
	for i, ts := range c.sortedTasks(statsSummaryKeep) {
		// 只给最近的几个任务存曲线，其余只留汇总（体积控制在这里）
		if i >= statsSeriesKeep {
			ts.Samples = nil
		}
		p.Tasks = append(p.Tasks, ts)
	}
	data, err := json.Marshal(&p)
	if err != nil {
		return nil, false // 序列化失败：保留 dirty，下次采样或退出时再试
	}
	c.dirty = false
	c.lastFlush = time.Now()
	return data, true
}

// flush 组装并落盘（两步都在锁内完成）。生产路径不用它——写盘阻塞在锁里；
// 保留给测试与一次性场景作为便捷入口。
func (c *statsCollector) flush(now time.Time, force bool) error {
	data, ok := c.payload(now, force)
	if !ok {
		return nil
	}
	return writeFileAtomic(c.path, data)
}

// snapshot 组装一次对外读数。当前速度取最后一个全局采样值（2s 粒度），
// 不用那一瞬时的速度合计：后者会因限速器整块放行配额在 0 与峰值之间跳。
func (c *statsCollector) snapshot(now time.Time, tasks []Task) Stats {
	s := Stats{
		GeneratedAt: now.UnixMilli(),
		WindowMs:    int64(statsGlobalPoints) * statsSampleEvery.Milliseconds(),
	}
	if n := len(c.global); n > 0 {
		s.CurrentSpeed = c.global[n-1].V
	}
	today := now.Format("2006-01-02")
	week := make(map[string]bool, 7)
	for i := 0; i < 7; i++ {
		week[now.AddDate(0, 0, -i).Format("2006-01-02")] = true
	}
	for _, d := range c.daily {
		s.TotalBytes += d.Bytes
		if week[d.Date] {
			s.WeekBytes += d.Bytes
		}
		if d.Date == today {
			s.TodayBytes = d.Bytes
			s.TodayCompleted = d.Completed
			s.TodayFailed = d.Failed
		}
	}
	var sum int64
	var active int
	for _, pt := range c.global {
		if pt.V > s.PeakSpeed {
			s.PeakSpeed = pt.V
		}
		if pt.V > 0 {
			sum += pt.V
			active++
		}
	}
	if active > 0 {
		s.AvgSpeed = sum / int64(active)
	}
	if len(c.global) > 0 {
		s.GlobalFrom = c.global[0].T
		s.GlobalTo = c.global[len(c.global)-1].T
	}
	for _, t := range tasks {
		s.TaskCount++
		switch t.Status {
		case StatusRunning:
			s.Running++
		case StatusQueued:
			s.Queued++
		case StatusCompleted:
			s.Completed++
		case StatusFailed:
			s.Failed++
		case StatusPaused:
			s.Paused++
		}
	}
	s.Daily = make([]DailyStat, 0, len(c.daily))
	for _, d := range c.daily {
		s.Daily = append(s.Daily, *d)
	}
	sort.Slice(s.Daily, func(i, j int) bool { return s.Daily[i].Date < s.Daily[j].Date })
	s.Global = append([]SpeedPoint(nil), c.global...)
	// 任务汇总不带采样点：列表只展示汇总，曲线按需经 GetTaskStats 单取。
	// 否则一次 GetStats 要背 40 个任务 × 240 个点，统计页每秒轮询就是数百 KB。
	taskStats := c.sortedTasks(statsPayloadTasks)
	for i := range taskStats {
		taskStats[i].Samples = nil
	}
	s.Tasks = taskStats
	return s
}

// GetStats 返回下载统计快照（总量、近 7 天、速度曲线与各任务汇总）。
// 曲线从内存取当前值；落盘的局部（全局曲线 + 最近几个任务的曲线）重启后仍在。
func (m *Manager) GetStats() Stats {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stats.snapshot(m.now(), m.snapshotLocked())
}

// GetTaskStats 返回单个任务的统计汇总与曲线（统计页选中某任务时按需拉取）。
// 调用方须持有 mu（由 Manager 方法包装）。
func (m *Manager) GetTaskStats(id string) (TaskStat, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ts, ok := m.stats.tasks[id]
	if !ok {
		return TaskStat{}, false
	}
	return *ts, true
}

// writeStatsData 把 payload 组装好的统计字节写盘（锁外调用）。
// 写失败时恢复 dirty：否则这批数据要等到下次采样才会重落，退出前的
// force flush 也会因「无变化」直接跳过它，失败即永久丢失。
func (m *Manager) writeStatsData(data []byte) {
	if len(data) == 0 {
		return
	}
	if err := writeFileAtomic(m.store.StatsPath(), data); err != nil {
		m.mu.Lock()
		m.stats.dirty = true
		m.mu.Unlock()
		fmt.Fprintln(os.Stderr, "保存统计失败:", err)
	}
}

// FlushStats 立即落盘统计数据（退出前调用，跳过节流）。
func (m *Manager) FlushStats() error {
	m.mu.Lock()
	now := m.now()
	m.stats.sample(m.snapshotLocked(), now)
	data, ok := m.stats.payload(now, true)
	m.mu.Unlock()
	if !ok {
		return nil
	}
	if err := writeFileAtomic(m.store.StatsPath(), data); err != nil {
		m.mu.Lock()
		m.stats.dirty = true
		m.mu.Unlock()
		fmt.Fprintln(os.Stderr, "保存统计失败:", err)
		return err
	}
	return nil
}

// Stop 只停掉后台进度循环，不落盘。幂等。
func (m *Manager) Stop() {
	select {
	case <-m.quit:
	default:
		close(m.quit)
	}
	// 等循环真退出：否则它可能正在写 stats.json，与后续操作撞车
	select {
	case <-m.loopDone:
	case <-time.After(2 * time.Second):
	}
}

// Close 停止后台进度循环并把统计落盘。幂等，可重复调用。
//
// 不提供会怎样：应用退出时最后一段采样丢在内存里；测试里进度循环会在
// t.TempDir() 清理的同时继续往数据目录写文件（Windows 上直接报目录非空），
// 而且每个用例漏一个 goroutine。正在跑的那一轮不在这里等，任务进度已由
// finishTask 落盘。测试收尾应用 Stop，否则会多一次刚写又被安全软件盯上的文件。
func (m *Manager) Close() error {
	m.Stop()
	return m.FlushStats()
}
