package api

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"

	"downloader/engine"
)

// Handler 构建路由。所有请求须通过 authorize：
//
//	GET    /api/v1/ping                       健康检查（免令牌）
//	GET    /api/v1/tasks                      任务列表（含实时进度）
//	POST   /api/v1/tasks                      新建任务，字段与 engine.AddTaskParams 一致（fileName 为 customName 别名）
//	POST   /api/v1/tasks/{id}/pause           暂停
//	POST   /api/v1/tasks/{id}/continue        恢复
//	DELETE /api/v1/tasks/{id}                 删除（files=1 连文件删除，目录须在白名单内）
//	POST   /api/v1/queue/pause-all            暂停全部（返回受影响任务数）
//	POST   /api/v1/queue/resume-all           恢复全部（含失败任务）
//	POST   /api/v1/queue/retry-failed         仅重试失败任务
//	POST   /api/v1/queue/clear-completed      清理已完成记录（保留磁盘文件）
//	PUT    /api/v1/queue/suspended            开关队列总闸 {"suspended": true}
//	GET    /api/v1/queue/status               队列放行状态（闸门/时间窗/空间不足停摆数）
//	PUT    /api/v1/queue/window               带宽时间窗 {"enabled":true,"start":"22:00","end":"06:00"}
//	GET    /api/v1/stats                      下载统计与速度曲线（今日/7 天/累计 + 最近曲线）
//	GET    /api/v1/stats/tasks/{id}            单任务速度曲线（任务无统计记录时 404）
//	GET    /api/v1/settings                   读取设置
//	PUT    /api/v1/settings                   保存设置
func (s *Server) Handler() http.Handler {
	b := s.backend
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	writeErr := func(w http.ResponseWriter, code int, msg string) {
		writeJSON(w, code, map[string]string{"error": msg})
	}
	// 引擎错误 → HTTP 状态码：任务不存在 404，状态冲突等其余 400
	writeTaskErr := func(w http.ResponseWriter, err error) {
		if errors.Is(err, engine.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
	}

	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": "downloader"})
	})

	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, b.GetTasks())
	})

	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		// 内嵌 AddTaskParams：校验和/优先级/定时/每任务限速等扩展字段与引擎同名同 tag
		var body struct {
			engine.AddTaskParams
			FileName string `json:"fileName"` // 已发布文档用的字段名，与 customName 同义
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		if body.FileName != "" {
			body.CustomName = body.FileName
		}
		if !b.SaveDirAllowed(body.SaveDir) {
			writeErr(w, http.StatusForbidden, "saveDir 不在允许的目录白名单内（可在设置中配置）")
			return
		}
		t, err := b.AddTaskWithChecksum(body.AddTaskParams)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, t)
	})

	mux.HandleFunc("POST /api/v1/tasks/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		if err := b.PauseTask(r.PathValue("id")); err != nil {
			writeTaskErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/v1/tasks/{id}/continue", func(w http.ResponseWriter, r *http.Request) {
		if err := b.ResumeTask(r.PathValue("id")); err != nil {
			writeTaskErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		deleteFiles := r.URL.Query().Get("files") == "1" || r.URL.Query().Get("files") == "true"
		if deleteFiles {
			// 连文件删除具有破坏性：目录不受白名单保护时拒绝经 API 触发
			dir, ok := b.TaskSaveDir(r.PathValue("id"))
			if !ok {
				writeErr(w, http.StatusNotFound, engine.ErrTaskNotFound.Error())
				return
			}
			if !b.SaveDirAllowed(dir) {
				writeErr(w, http.StatusForbidden, "该任务目录不在白名单内，不能通过 API 连文件删除")
				return
			}
		}
		if err := b.RemoveTask(r.PathValue("id"), deleteFiles); err != nil {
			writeTaskErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	// 队列级批量操作：与单任务 pause/continue 一样无请求体，返回受影响任务数
	queuePost := func(path string, fn func() int) {
		mux.HandleFunc("POST "+path, func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, map[string]int{"count": fn()})
		})
	}
	queuePost("/api/v1/queue/pause-all", b.PauseAll)
	queuePost("/api/v1/queue/resume-all", b.ResumeAll)
	queuePost("/api/v1/queue/retry-failed", b.RetryAllFailed)
	queuePost("/api/v1/queue/clear-completed", b.ClearCompleted)

	mux.HandleFunc("PUT /api/v1/queue/suspended", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Suspended bool `json:"suspended"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		if err := b.SetQueueSuspended(body.Suspended); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"suspended": b.GetSettings().QueueSuspended})
	})

	// 队列为什么不动：用户闭闸 / 带宽时间窗外 / 磁盘空间不足，三者分开报
	mux.HandleFunc("GET /api/v1/queue/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, b.QueueStatus())
	})

	mux.HandleFunc("PUT /api/v1/queue/window", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Enabled bool   `json:"enabled"`
			Start   string `json:"start"` // "HH:MM"，本地时间
			End     string `json:"end"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		// 时间格式非法属于调用方错误，不记 500
		if err := b.SetQueueWindow(body.Enabled, body.Start, body.End); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, b.QueueStatus())
	})

	mux.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, b.GetSettings())
	})

	mux.HandleFunc("GET /api/v1/stats", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, b.GetStats())
	})

	// 单任务曲线按需拉取：GET /api/v1/stats 的汇总不再带 samples，
	// 前端选中某任务时才走这里
	mux.HandleFunc("GET /api/v1/stats/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		ts, ok := b.GetTaskStats(r.PathValue("id"))
		if !ok {
			writeErr(w, http.StatusNotFound, "该任务暂无统计记录")
			return
		}
		writeJSON(w, http.StatusOK, ts)
	})

	mux.HandleFunc("PUT /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var st engine.Settings
		if err := json.NewDecoder(r.Body).Decode(&st); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		if err := b.SaveSettings(st); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		// API 开关/端口可能变了：异步启停（不能同步——Shutdown 会等本请求返回）
		go s.Sync()
		writeJSON(w, http.StatusOK, b.GetSettings())
	})

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := s.authorize(r); err != nil {
			code := http.StatusForbidden
			if errors.Is(err, errAPIUnauthorized) {
				code = http.StatusUnauthorized
			}
			writeErr(w, code, err.Error())
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// authorize 对请求做本机边界校验。绑定 127.0.0.1 只挡住了外部主机，
// 挡不住浏览器里的恶意网页（CSRF）与 DNS rebinding，这里逐层补齐：
//
//  1. Host 必须是回环地址——DNS rebinding 会把恶意域名解析到 127.0.0.1，
//     但浏览器发出的 Host 头仍是那个恶意域名，可直接识破。
//  2. Origin 非空时必须是回环地址——浏览器跨站请求（即使是免预检的
//     "简单请求"）也会携带 Origin；本机自动化脚本不发送 Origin，不受影响。
//  3. 带请求体的 POST/PUT 要求 Content-Type 为 application/json——
//     浏览器不允许简单请求伪造该类型，攻击者无法绕过（HTML 表单只能发
//     text/plain / form-urlencoded / multipart）。
//  4. 除 ping 外要求令牌：X-API-Token 或 Authorization: Bearer，
//     恒定时间比较；令牌在开启 API 时自动生成（设置页可见）。
func (s *Server) authorize(r *http.Request) error {
	host := strings.ToLower(r.Host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	switch host {
	case "127.0.0.1", "localhost", "::1":
	default:
		return errors.New("仅允许本机访问（Host: " + r.Host + "）")
	}

	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		if err != nil {
			return errors.New("拒绝跨站请求（Origin: " + o + "）")
		}
		switch strings.ToLower(u.Hostname()) {
		case "127.0.0.1", "localhost", "::1":
		default:
			return errors.New("拒绝跨站请求（Origin: " + o + "）")
		}
	}

	if r.ContentLength != 0 {
		ct, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || !strings.EqualFold(ct, "application/json") {
			return errors.New("Content-Type 必须为 application/json")
		}
	}

	if r.URL.Path == "/api/v1/ping" {
		return nil
	}
	got := strings.TrimSpace(r.Header.Get("X-API-Token"))
	if got == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			got = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer "))
		}
	}
	want := s.backend.GetSettings().APIToken
	if want == "" || got == "" || subtle.ConstantTimeCompare([]byte(got), []byte(want)) != 1 {
		return errAPIUnauthorized
	}
	return nil
}
