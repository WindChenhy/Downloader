package main

type Chunk struct {
	Index int
	Start int64
	End   int64
}

func CalculateChunks(fileSize, chunkSize int64, threads int) []Chunk {
	if chunkSize <= 0 {
		chunkSize = fileSize / int64(threads)
		if chunkSize <= 0 {
			chunkSize = fileSize
		}
	}

	var chunks []Chunk
	var offset int64
	index := 0

	for offset < fileSize {
		end := offset + chunkSize - 1
		if end >= fileSize {
			end = fileSize - 1
		}
		chunks = append(chunks, Chunk{
			Index: index,
			Start: offset,
			End:   end,
		})
		offset = end + 1
		index++
	}
	return chunks
}
