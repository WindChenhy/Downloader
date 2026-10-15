// Package api 提供本地 REST API：请求鉴权守卫、路由与按设置启停的服务生命周期。
// 引擎只暴露能力（Backend），不持有 HTTP 服务；本包反向依赖 engine 的类型，
// 因此服务器的启停由应用层（App）与 PUT /settings 处理器驱动，engine 不回引本包。
package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"downloader/engine"
)

// Backend REST API 需要的引擎能力，由 *engine.Manager 实现。
type Backend interface {
	GetTasks() []engine.Task
	AddTaskWithChecksum(p engine.AddTaskParams) (engine.Task, error)
	PauseTask(id string) error
	ResumeTask(id string) error
	RemoveTask(id string, deleteFiles bool) error
	// 队列级批量操作（见 engine/queueops.go）：返回值为本轮受影响的任务数。
	PauseAll() int
	ResumeAll() int
	RetryAllFailed() int
	ClearCompleted() int
	SetQueueSuspended(v bool) error
	// QueueStatus / SetQueueWindow 队列放行状态与带宽时间窗（窗外不启动新任务）。
	QueueStatus() engine.QueueStatus
	SetQueueWindow(enabled bool, start, end string) error
	// GetStats 下载统计与速度曲线（见 engine/stats.go）。
	GetStats() engine.Stats
	// GetTaskStats 单任务曲线：汇总（GetStats）已剥离 Samples，
	// 曲线改由本接口按任务 ID 按需拉取。
	GetTaskStats(id string) (engine.TaskStat, bool)
	GetSettings() engine.Settings
	SaveSettings(s engine.Settings) error
	// SaveDirAllowed 目录是否在「默认下载目录 + 白名单」内（写/删文件类操作的前置校验）。
	SaveDirAllowed(dir string) bool
	// TaskSaveDir 返回任务保存目录；任务不存在时 ok 为 false。
	TaskSaveDir(id string) (string, bool)
}

// errAPIUnauthorized 缺少/错误的 API 令牌（HTTP 401）；其余鉴权失败一律 403。
var errAPIUnauthorized = errors.New("缺少或错误的 API 令牌：请携带 X-API-Token 或 Authorization: Bearer 头")

// Server 本地 REST API 服务：仅绑定 127.0.0.1，按设置启停。
// Sync 必须在请求处理路径之外调用（如新 goroutine）——Shutdown 会等待
// 在途请求返回，从处理器内同步调用会自我等待。
type Server struct {
	backend Backend

	mu   sync.Mutex
	srv  *http.Server
	addr string
}

// New 创建服务（不启动；用 Sync 按当前设置拉起）。
func New(backend Backend) *Server { return &Server{backend: backend} }

// Sync 按当前设置启停服务：端口或开关变化时先优雅停掉旧实例再按需重启。
// 应用启动与设置保存后调用；不要从请求处理路径内同步调用。
func (s *Server) Sync() {
	st := s.backend.GetSettings()

	wantAddr := ""
	if st.APIEnabled && st.APIPort > 0 {
		wantAddr = fmt.Sprintf("127.0.0.1:%d", st.APIPort)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.srv != nil && s.addr == wantAddr {
		return // 无变化
	}
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_ = s.srv.Shutdown(ctx)
		cancel()
		s.srv = nil
		s.addr = ""
	}
	if wantAddr == "" {
		return
	}
	srv := &http.Server{
		Addr:              wantAddr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	s.srv = srv
	s.addr = wantAddr
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fmt.Fprintln(os.Stderr, "REST API 启动失败:", err)
			// 启动失败（端口被占用等）要把实例摘掉，否则 srv 与 addr 都还在，
			// 后续 Sync 会认为"地址没变、已在运行"而直接返回，端口空出来后
			// 也不会再试一次
			s.mu.Lock()
			if s.srv == srv {
				s.srv = nil
				s.addr = ""
			}
			s.mu.Unlock()
		}
	}()
}
