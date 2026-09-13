package engine

// Chunk 表示一个闭区间分段 [Start, End]。
type Chunk struct {
	Index int
	Start int64
	End   int64
}

// CalculateChunks 把 [0, fileSize) 切成 numChunks 个尽量均衡的分段，
// 各分段首尾相接、完整覆盖整个区间。
func CalculateChunks(fileSize int64, numChunks int) []Chunk {
	if fileSize <= 0 || numChunks < 1 {
		return nil
	}
	if int64(numChunks) > fileSize {
		numChunks = int(fileSize)
	}
	chunks := make([]Chunk, 0, numChunks)
	base := fileSize / int64(numChunks)
	rem := fileSize % int64(numChunks)
	var start int64
	for i := 0; i < numChunks; i++ {
		size := base
		if int64(i) < rem {
			size++
		}
		chunks = append(chunks, Chunk{Index: i, Start: start, End: start + size - 1})
		start += size
	}
	return chunks
}
