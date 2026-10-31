package engine

import (
	"testing"
)

func TestCalculateChunks(t *testing.T) {
	cases := []struct {
		name      string
		size      int64
		n         int
		wantLen   int
		wantFirst Chunk
	}{
		{"均匀切分", 10, 3, 3, Chunk{0, 0, 3}},
		{"单段", 1, 1, 1, Chunk{0, 0, 0}},
		{"段数大于字节数", 2, 8, 2, Chunk{0, 0, 0}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := CalculateChunks(c.size, c.n)
			if len(got) != c.wantLen {
				t.Fatalf("段数 = %d, want %d", len(got), c.wantLen)
			}
			if got[0] != c.wantFirst {
				t.Fatalf("第一段 = %+v, want %+v", got[0], c.wantFirst)
			}
			// 完整覆盖且互不重叠
			var next int64
			for _, ch := range got {
				if ch.Start != next {
					t.Fatalf("分段不连续: 段 Start=%d, want %d", ch.Start, next)
				}
				if ch.End < ch.Start {
					t.Fatalf("非法分段: %+v", ch)
				}
				next = ch.End + 1
			}
			if next != c.size {
				t.Fatalf("覆盖到 %d, want %d", next, c.size)
			}
		})
	}

	if got := CalculateChunks(0, 4); got != nil {
		t.Fatalf("size=0 应返回 nil, got %+v", got)
	}
	if got := CalculateChunks(-1, 4); got != nil {
		t.Fatalf("size<0 应返回 nil, got %+v", got)
	}
}

func TestSidecarDoneBytesAndComplete(t *testing.T) {
	sc := &Sidecar{Chunks: []SidecarChunk{
		{Start: 0, End: 9, Done: true},
		{Start: 10, End: 19, Done: false},
		{Start: 20, End: 29, Done: true},
	}}
	if got := sc.DoneBytes(); got != 20 {
		t.Fatalf("DoneBytes = %d, want 20", got)
	}
	if sc.Complete() {
		t.Fatal("还有未完成分段, 不应 Complete")
	}
	sc.Chunks[1].Done = true
	if !sc.Complete() {
		t.Fatal("全部分段完成后应 Complete")
	}
	if got := (&Sidecar{}).DoneBytes(); got != 0 {
		t.Fatalf("空 Sidecar DoneBytes = %d, want 0", got)
	}
	if (&Sidecar{}).Complete() {
		t.Fatal("空 Sidecar 不应 Complete")
	}
}
