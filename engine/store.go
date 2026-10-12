package engine

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Store 管理应用数据目录下的持久化文件：
//
//	settings.json     全局设置
//	tasks.json        任务列表
//	stats.json        下载统计（按天聚合 + 最近的速度曲线）
//	state/<id>.json   各任务的断点续传状态（Sidecar）
type Store struct {
	dir string
}

func NewStore(dir string) *Store { return &Store{dir: dir} }

func (s *Store) SettingsPath() string { return filepath.Join(s.dir, "settings.json") }
func (s *Store) TasksPath() string    { return filepath.Join(s.dir, "tasks.json") }
func (s *Store) StatsPath() string    { return filepath.Join(s.dir, "stats.json") }
func (s *Store) StatePath(taskID string) string {
	return filepath.Join(s.dir, "state", taskID+".json")
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, data)
}

// writeFileAtomic 临时文件 + 重命名的原子写。权限取 0o600：数据目录里的
// settings.json 含 API 令牌、tasks.json 含每任务 Cookie，都是本机敏感数据，
// 不应被同机其他用户读到（Windows 上由 ACL 主导，Unix 上这几位是全部屏障）。
func writeFileAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
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
	MaxConnections         = 128
	DefaultConcurrentTasks = 3
	MaxConcurrentTasks     = 10

	ProxyNone   = "none"
	ProxySystem = "system"
	ProxyCustom = "custom"

	DefaultMirrorTemplate = "https://gh-proxy.com/{url}"
	DefaultAPIPort        = 8199

	CloseActionAsk      = "ask"      // 每次询问
	CloseActionExit     = "exit"     // 直接退出
	CloseActionMinimize = "minimize" // 最小化到系统托盘
)

// DirCategory 下载目录分类：按类型把文件落到不同子目录。
type DirCategory struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// Settings 全局设置。
type Settings struct {
	SaveDir         string `json:"saveDir"`
	Connections     int    `json:"connections"`
	ConcurrentTasks int    `json:"concurrentTasks"`
	SpeedLimit      int64  `json:"speedLimit"`     // 全局下载限速，字节/秒，0 不限
	UserAgent       string `json:"userAgent"`      // 空 = 内置默认 UA
	ExtraHeaders    string `json:"extraHeaders"`   // 自定义请求头，每行一条 "Key: Value"
	ProxyMode       string `json:"proxyMode"`      // none | system | custom
	ProxyURL        string `json:"proxyUrl"`       // custom 模式生效，如 http://127.0.0.1:7890
	GitHubMirror    bool   `json:"githubMirror"`   // GitHub 链接自动走镜像加速
	MirrorTemplate  string `json:"mirrorTemplate"` // 镜像模板，{url} 为原始链接
	ClipboardWatch  bool   `json:"clipboardWatch"` // 剪贴板监听
	APIEnabled      bool   `json:"apiEnabled"`     // 本地 REST API
	APIPort         int    `json:"apiPort"`
	// REST API 访问令牌（X-API-Token / Bearer）；由应用自动生成，
	// 保存设置时若留空则保留现有值，不经表单清空
	APIToken string `json:"apiToken,omitempty"`
	// API/导入允许的保存目录白名单（默认下载目录始终允许）；
	// 空 = 仅默认下载目录。用于阻断经接口向任意目录写/删文件
	AllowSaveDirs []string      `json:"allowSaveDirs,omitempty"`
	CloseAction   string        `json:"closeAction"`   // ask | exit | minimize
	DirCategories []DirCategory `json:"dirCategories"` // 下载目录分类
	AutoExtract   bool          `json:"autoExtract"`   // 下载完成后自动解压压缩包
	// 系统通知：完成/失败默认开，创建/暂停默认关，均可配置
	NotifyOnCreate   bool `json:"notifyOnCreate"`
	NotifyOnPause    bool `json:"notifyOnPause"`
	NotifyOnComplete bool `json:"notifyOnComplete"`
	NotifyOnFail     bool `json:"notifyOnFail"`
	// AfterComplete 全部任务结束后的动作
	AfterComplete string `json:"afterComplete"`
	// Language 界面语言（zh-CN / en-US）：由前端解析"跟随系统"后同步而来，
	// 供系统通知与托盘菜单使用
	Language string `json:"language"`
	// QueueSuspended 队列总闸：true 时不启动任何任务，已排队的任务停在 queued。
	// 持久化，重启后保持闭闸；旧设置文件缺省该字段即为开闸（沿用原行为）
	QueueSuspended bool `json:"queueSuspended"`
	// WindowEnabled 带宽时间窗：启用后只在 [WindowStart, WindowEnd) 内启动新任务，
	// 正在下载的任务不受影响。窗外与用户闭闸走同一条判定（见 dispatchLocked）
	WindowEnabled bool   `json:"windowEnabled"`
	WindowStart   string `json:"windowStart"` // "HH:MM"，本地时间
	WindowEnd     string `json:"windowEnd"`   // "HH:MM"；与起点相同 = 全天放行
	// DiskGuardOff 关闭下载前的磁盘空间预检查。字段刻意取反：老设置文件里没有这个键，
	// 解码得到 false = 预检查开启（新字段默认值应为“开”的都这么写）
	DiskGuardOff bool `json:"diskGuardOff"`
	// DiskMinFree 预检查在文件大小之外还要为用户保留的余量（字节），0 = 不留余量
	DiskMinFree int64 `json:"diskMinFree"`
}

func defaultSaveDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return filepath.Join(home, "Downloads")
}

// defaultDirCategories 首次运行预置的目录分类（与常见下载场景对齐）。
func defaultDirCategories() []DirCategory {
	base := filepath.Join(defaultSaveDir())
	return []DirCategory{
		{Name: "音乐", Path: filepath.Join(base, "Music")},
		{Name: "视频", Path: filepath.Join(base, "Video")},
		{Name: "文档", Path: filepath.Join(base, "Document")},
		{Name: "程序", Path: filepath.Join(base, "Program")},
	}
}

// defaultSettings 首次运行（无设置文件）时的默认值。
func defaultSettings() Settings {
	return Settings{
		SaveDir:          defaultSaveDir(),
		Connections:      DefaultConnections,
		ConcurrentTasks:  DefaultConcurrentTasks,
		ProxyMode:        ProxyNone,
		MirrorTemplate:   DefaultMirrorTemplate,
		ClipboardWatch:   true,
		APIEnabled:       true,
		APIPort:          DefaultAPIPort,
		CloseAction:      CloseActionAsk,
		DirCategories:    defaultDirCategories(),
		AutoExtract:      false,
		NotifyOnCreate:   false,
		NotifyOnPause:    false,
		NotifyOnComplete: true,
		NotifyOnFail:     true,
		AfterComplete:    AfterCompleteNone,
		// 磁盘预检查默认开（DiskGuardOff 的零值即“开”）；余量默认 0，
		// 只在真的装不下时才拦，要留安全边界由用户在设置里加
	}
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
	if s.SpeedLimit < 0 {
		s.SpeedLimit = 0
	}
	if s.DiskMinFree < 0 {
		s.DiskMinFree = 0
	}
	// 时间窗只接受 HH:MM；非法值回落到 00:00（与另一项相同时 = 全天放行），
	// 宁可不管也不要把队列永久锁死
	if _, ok := parseHHMM(s.WindowStart); !ok {
		s.WindowStart = "00:00"
	}
	if _, ok := parseHHMM(s.WindowEnd); !ok {
		s.WindowEnd = "00:00"
	}
	switch s.ProxyMode {
	case ProxySystem, ProxyCustom:
	default:
		s.ProxyMode = ProxyNone
	}
	if strings.TrimSpace(s.MirrorTemplate) == "" {
		s.MirrorTemplate = DefaultMirrorTemplate
	}
	if s.APIPort < 0 {
		s.APIPort = 0
	}
	if s.APIPort > 65535 {
		s.APIPort = 65535
	}
	if s.APIEnabled && s.APIPort == 0 {
		s.APIPort = DefaultAPIPort
	}
	// 目录白名单：逐项清洗，去掉空白与退化项
	if len(s.AllowSaveDirs) > 0 {
		out := make([]string, 0, len(s.AllowSaveDirs))
		for _, d := range s.AllowSaveDirs {
			d = filepath.Clean(strings.TrimSpace(d))
			if d != "" && d != "." {
				out = append(out, d)
			}
		}
		if len(out) == 0 {
			out = nil
		}
		s.AllowSaveDirs = out
	}
	// 关闭行为：非法值回落为询问；默认绝不是最小化
	switch s.CloseAction {
	case CloseActionExit, CloseActionMinimize:
	default:
		s.CloseAction = CloseActionAsk
	}
	switch s.AfterComplete {
	case AfterCompleteOpenDir, AfterCompleteShutdown, AfterCompleteSleep, AfterCompleteExitApp:
	default:
		s.AfterComplete = AfterCompleteNone
	}
	// 界面语言：非法值回落为中文
	switch s.Language {
	case "en-US":
	default:
		s.Language = "zh-CN"
	}
	// 目录分类：去掉空白项，名称去重（同名后者覆盖路径）
	if s.DirCategories == nil {
		s.DirCategories = nil
	} else {
		seen := make(map[string]int)
		out := make([]DirCategory, 0, len(s.DirCategories))
		for _, c := range s.DirCategories {
			name := strings.TrimSpace(c.Name)
			path := strings.TrimSpace(c.Path)
			if name == "" || path == "" {
				continue
			}
			if i, ok := seen[name]; ok {
				out[i] = DirCategory{Name: name, Path: path}
				continue
			}
			seen[name] = len(out)
			out = append(out, DirCategory{Name: name, Path: path})
		}
		s.DirCategories = out
	}
}

func (s *Store) LoadSettings() (Settings, error) {
	var st Settings
	raw, err := os.ReadFile(s.SettingsPath())
	if errors.Is(err, fs.ErrNotExist) {
		st = defaultSettings()
		return st, nil
	}
	if err != nil {
		st = defaultSettings()
		return st, err
	}
	if err := json.Unmarshal(raw, &st); err != nil {
		st = defaultSettings()
		return st, err
	}
	// 旧版本设置文件没有这些字段：补上首启默认值而不是关掉功能
	var keys map[string]json.RawMessage
	if json.Unmarshal(raw, &keys) == nil {
		if _, ok := keys["clipboardWatch"]; !ok {
			st.ClipboardWatch = true
		}
		if _, ok := keys["apiEnabled"]; !ok {
			st.APIEnabled = true
		}
		if _, ok := keys["dirCategories"]; !ok {
			st.DirCategories = defaultDirCategories()
		}
		if _, ok := keys["notifyOnComplete"]; !ok {
			st.NotifyOnComplete = true
		}
		if _, ok := keys["notifyOnFail"]; !ok {
			st.NotifyOnFail = true
		}
	}
	st.normalize()
	return st, nil
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

var idCounter atomic.Uint32

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// 兜底：随机源不可用时用 时间戳+进程号+进程内计数，避免同秒并发创建撞键
		return fmt.Sprintf("%s-%d-%08x", time.Now().Format("20060102150405"), os.Getpid(), idCounter.Add(1))
	}
	return hex.EncodeToString(b[:])
}
