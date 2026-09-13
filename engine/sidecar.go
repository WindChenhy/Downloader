package engine

import (
	"encoding/json"
	"os"
)

// Sidecar 记录单个任务的断点续传状态，保存在应用数据目录 state/<taskID>.json。
// 数据本体写在 <saveDir>/<fileName>.part，全部完成后才改名为正式文件。
type Sidecar struct {
	URL          string         `json:"url"`
	TotalSize    int64          `json:"totalSize"`
	ETag         string         `json:"etag,omitempty"`
	LastModified string         `json:"lastModified,omitempty"`
	Single       bool           `json:"single,omitempty"` // 单连接模式：续传偏移取 .part 文件大小
	Chunks       []SidecarChunk `json:"chunks"`
}

// SidecarChunk 一个分段的续传状态，[Start, End] 为闭区间。
// Received 记录段内已落盘的字节数：暂停/重试/恢复都从这里接着下（字节级续传）。
type SidecarChunk struct {
	Start    int64 `json:"start"`
	End      int64 `json:"end"` // 大小未知时为 -1
	Done     bool  `json:"done"`
	Received int64 `json:"received,omitempty"`
}

// Size 返回分段总字节数。
func (c SidecarChunk) Size() int64 {
	if c.End < c.Start {
		return 0
	}
	return c.End - c.Start + 1
}

func (s *Sidecar) DoneBytes() int64 {
	var n int64
	for _, c := range s.Chunks {
		if c.Done {
			n += c.Size()
		}
	}
	return n
}

// ProgressBytes 已下载字节数 = 完成分段 + 未完成分段内已落盘的部分。
func (s *Sidecar) ProgressBytes() int64 {
	var n int64
	for _, c := range s.Chunks {
		if c.Done {
			n += c.Size()
			continue
		}
		if c.Received > c.Size() {
			n += c.Size()
			continue
		}
		n += c.Received
	}
	return n
}

func (s *Sidecar) Complete() bool {
	if len(s.Chunks) == 0 {
		return false
	}
	for _, c := range s.Chunks {
		if !c.Done {
			return false
		}
	}
	return true
}

func saveSidecar(path string, s *Sidecar) error {
	return writeJSONAtomic(path, s)
}

func loadSidecar(path string) (*Sidecar, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s Sidecar
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, err
	}
	return &s, nil
}
