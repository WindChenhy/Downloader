package engine

import (
	"time"

	"downloader/sysaction"
)

// Status 任务状态机：
// queued → running → completed | failed | paused
// paused | failed → queued（恢复）；定时任务在 StartAt 前保持 queued。
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Priority 任务优先级：高优先出队。
const (
	PriorityLow    = 0
	PriorityNormal = 1
	PriorityHigh   = 2
)

// AfterCompleteAction 全部下载结束后的动作（设置级）。
// 取值定义在 sysaction 包（动作的执行方），此处引用保持单一来源。
const (
	AfterCompleteNone     = sysaction.None
	AfterCompleteOpenDir  = sysaction.OpenDir
	AfterCompleteShutdown = sysaction.Shutdown
	AfterCompleteSleep    = sysaction.Sleep
	AfterCompleteExitApp  = sysaction.ExitApp
)

// Task 下载任务。Speed 字段由 Manager 的进度循环实时填充，不持久化。
type Task struct {
	ID          string `json:"id"`
	URL         string `json:"url"`
	FileName    string `json:"fileName"`
	CustomName  string `json:"customName,omitempty"` // 用户指定的文件名；空 = 自动获取
	SaveDir     string `json:"saveDir"`
	TotalSize   int64  `json:"totalSize"` // <=0 表示大小未知
	Downloaded  int64  `json:"downloaded"`
	Speed       int64  `json:"speed"` // 字节/秒
	Status      Status `json:"status"`
	Connections int    `json:"connections"`
	Error       string `json:"error,omitempty"`
	// BlockReason 任务停在队列里却没被启动的原因（目前只有磁盘空间不足）；
	// 空 = 无阻塞。状态仍是 queued（不是失败），预检查每 2s 复检一次，
	// 空间腾出来就自己起跑；重启后由预检查重新算出，无需人工重试。
	BlockReason string    `json:"blockReason,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	// ActiveMs 纯下载耗时（毫秒）：仅累计 running 阶段，暂停/排队不计。
	ActiveMs int64 `json:"activeMs"`
	// FinishedAt 任务结束时刻（完成或失败）；零值表示尚未结束。
	// 总耗时 = FinishedAt - CreatedAt（未结束时用当前时间），中间暂停也计入。
	FinishedAt time.Time `json:"finishedAt"`
	// AvgSpeed 平均下载速度（字节/秒），按 Downloaded / ActiveMs 计算。
	AvgSpeed int64 `json:"avgSpeed"`
	// Priority 优先级（0 低 / 1 普通 / 2 高）；同并发下高优先出队。
	Priority int `json:"priority"`
	// StartAt 定时开始时刻；零值表示立即。未到点的任务不进入 running。
	StartAt time.Time `json:"startAt"`
	// SpeedLimit 每任务限速（字节/秒）；0 跟随全局。
	SpeedLimit int64 `json:"speedLimit"`
	// Cookie / Referer 每任务的认证与防盗链头；空 = 沿用全局自定义请求头。
	// 明文存在 tasks.json（用户显式填写视为知情），导出任务 JSON 时默认剥离
	Cookie  string `json:"cookie,omitempty"`
	Referer string `json:"referer,omitempty"`
	// 校验和（可选）：用户可提供期望值；完成后写入实际摘要并标记状态。
	ChecksumAlgo     string `json:"checksumAlgo,omitempty"`     // md5 | sha1 | sha256
	ChecksumExpected string `json:"checksumExpected,omitempty"` // 期望摘要，十六进制
	ChecksumActual   string `json:"checksumActual,omitempty"`   // 完成后计算得到
	ChecksumStatus   string `json:"checksumStatus,omitempty"`   // ok | mismatch | error | skipped
}

const (
	ChecksumOK       = "ok"
	ChecksumMismatch = "mismatch"
	ChecksumError    = "error"
	ChecksumSkipped  = "skipped"
)
