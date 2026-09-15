package engine

import "time"

// Status 任务状态机：queued -> running -> completed / failed / paused
type Status string

const (
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
)

// Task 下载任务。Speed 字段由 Manager 的进度循环实时填充，不持久化。
type Task struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	FileName    string    `json:"fileName"`
	CustomName  string    `json:"customName,omitempty"` // 用户指定的文件名；空 = 自动获取
	SaveDir     string    `json:"saveDir"`
	TotalSize   int64     `json:"totalSize"` // <=0 表示大小未知
	Downloaded  int64     `json:"downloaded"`
	Speed       int64     `json:"speed"` // 字节/秒
	Status      Status    `json:"status"`
	Connections int       `json:"connections"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
	// ActiveMs 纯下载耗时（毫秒）：仅累计 running 阶段，暂停/排队不计。
	ActiveMs int64 `json:"activeMs"`
	// FinishedAt 任务结束时刻（完成或失败）；零值表示尚未结束。
	// 总耗时 = FinishedAt - CreatedAt（未结束时用当前时间），中间暂停也计入。
	FinishedAt time.Time `json:"finishedAt"`
	// AvgSpeed 平均下载速度（字节/秒），按 Downloaded / ActiveMs 计算。
	AvgSpeed int64 `json:"avgSpeed"`
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
