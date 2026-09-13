package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// APIHandler 构建本地 REST API（仅绑定 127.0.0.1，供自动化与远程管理使用）：
//
//	GET    /api/v1/ping                       健康检查
//	GET    /api/v1/tasks                      任务列表（含实时进度）
//	POST   /api/v1/tasks                      新建任务 {url, saveDir?, connections?}
//	POST   /api/v1/tasks/{id}/pause           暂停
//	POST   /api/v1/tasks/{id}/continue        恢复
//	DELETE /api/v1/tasks/{id}                 删除
//	GET    /api/v1/settings                   读取设置
//	PUT    /api/v1/settings                   保存设置
func (m *Manager) APIHandler() http.Handler {
	mux := http.NewServeMux()

	writeJSON := func(w http.ResponseWriter, code int, v any) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(v)
	}
	writeErr := func(w http.ResponseWriter, code int, msg string) {
		writeJSON(w, code, map[string]string{"error": msg})
	}

	mux.HandleFunc("GET /api/v1/ping", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "app": "downloader"})
	})

	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.GetTasks())
	})

	mux.HandleFunc("POST /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			URL         string `json:"url"`
			SaveDir     string `json:"saveDir"`
			Connections int    `json:"connections"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		t, err := m.AddTask(body.URL, body.SaveDir, body.Connections)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, t)
	})

	mux.HandleFunc("POST /api/v1/tasks/{id}/pause", func(w http.ResponseWriter, r *http.Request) {
		if err := m.PauseTask(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("POST /api/v1/tasks/{id}/continue", func(w http.ResponseWriter, r *http.Request) {
		if err := m.ResumeTask(r.PathValue("id")); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("DELETE /api/v1/tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		deleteFiles := r.URL.Query().Get("files") == "1" || r.URL.Query().Get("files") == "true"
		if err := m.RemoveTask(r.PathValue("id"), deleteFiles); err != nil {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, m.GetSettings())
	})

	mux.HandleFunc("PUT /api/v1/settings", func(w http.ResponseWriter, r *http.Request) {
		var s Settings
		if err := json.NewDecoder(r.Body).Decode(&s); err != nil {
			writeErr(w, http.StatusBadRequest, "无效的请求体")
			return
		}
		if err := m.SaveSettings(s); err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, m.GetSettings())
	})

	return mux
}

// StartAPI 在本地地址启动 REST API 服务，goroutine 中运行、立即返回。
// 端口修改需重启应用生效。
func (m *Manager) StartAPI(addr string) *http.Server {
	srv := &http.Server{
		Addr:              addr,
		Handler:           m.APIHandler(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintln(os.Stderr, "REST API 启动失败:", err)
		}
	}()
	return srv
}
