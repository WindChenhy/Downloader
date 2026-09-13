package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store 管理应用数据目录下的持久化文件：
//
//	settings.json     全局设置
//	tasks.json        任务列表
//	state/<id>.json   各任务的断点续传状态（Sidecar）
type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) SettingsPath() string { return filepath.Join(s.dir, "settings.json") }
func (s *Store) TasksPath() string    { return filepath.Join(s.dir, "tasks.json") }
func (s *Store) StatePath(taskID string) string {
	return filepath.Join(s.dir, "state", taskID+".json")
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	// Windows 上目标文件可能被杀毒/备份/索引等外部进程短暂占用，
	// 重命名会报 sharing violation，短暂重试即可恢复
	var rerr error
	for i := 0; i < 10; i++ {
		rerr = os.Rename(tmp, path)
		if rerr == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return rerr
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

const (
	DefaultConnections     = 8
	MaxConnections         = 32
	DefaultConcurrentTasks = 3
	MaxConcurrentTasks     = 10
)

// Settings 全局设置。
type Settings struct {
	SaveDir         string `json:"saveDir"`
	Connections     int    `json:"connections"`
	ConcurrentTasks int    `json:"concurrentTasks"`
}

func defaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

func (s *Settings) normalize() {
	s.SaveDir = strings.TrimSpace(s.SaveDir)
	if s.SaveDir == "" {
		s.SaveDir = defaultSaveDir()
	}
	if s.Connections <= 0 {
		s.Connections = DefaultConnections
	}
	if s.Connections > MaxConnections {
		s.Connections = MaxConnections
	}
	if s.ConcurrentTasks <= 0 {
		s.ConcurrentTasks = DefaultConcurrentTasks
	}
	if s.ConcurrentTasks > MaxConcurrentTasks {
		s.ConcurrentTasks = MaxConcurrentTasks
	}
}

func (s *Store) LoadSettings() (Settings, error) {
	var st Settings
	err := readJSON(s.SettingsPath(), &st)
	if errors.Is(err, fs.ErrNotExist) {
		st, err = Settings{}, nil
	}
	st.normalize()
	return st, err
}

func (s *Store) SaveSettings(st Settings) error {
	st.normalize()
	return writeJSONAtomic(s.SettingsPath(), &st)
}

func (s *Store) LoadTasks() ([]Task, error) {
	var tasks []Task
	err := readJSON(s.TasksPath(), &tasks)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return tasks, err
}

func (s *Store) SaveTasks(tasks []Task) error {
	return writeJSONAtomic(s.TasksPath(), tasks)
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return time.Now().Format("20060102150405") // 兜底：随机源不可用时退化为时间戳
	}
	return hex.EncodeToString(b[:])
}
