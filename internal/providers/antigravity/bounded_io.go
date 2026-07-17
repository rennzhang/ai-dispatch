package antigravity

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	agyStdoutHeadLimitBytes     = 64 << 10
	agyStdoutTailLimitBytes     = 1 << 20
	agyStderrHeadLimitBytes     = 64 << 10
	agyStderrTailLimitBytes     = 256 << 10
	agyLogHeadLimitBytes        = 128 << 10
	agyLogTailLimitBytes        = 512 << 10
	agyTranscriptReadLimitBytes = 4 << 20
	agyTranscriptLineLimitBytes = agyTranscriptReadLimitBytes
)

const boundedGapMarker = "\n...[ai-dispatch retained bounded head and tail]...\n"

type boundedHeadTailBuffer struct {
	headLimit int
	tailLimit int
	total     int64
	head      []byte
	tail      []byte
}

func newBoundedHeadTailBuffer(headLimit int, tailLimit int) *boundedHeadTailBuffer {
	return &boundedHeadTailBuffer{headLimit: headLimit, tailLimit: tailLimit}
}

func (b *boundedHeadTailBuffer) Write(p []byte) (int, error) {
	written := len(p)
	b.total += int64(written)
	if remaining := b.headLimit - len(b.head); remaining > 0 {
		if remaining > len(p) {
			remaining = len(p)
		}
		b.head = append(b.head, p[:remaining]...)
	}
	if b.tailLimit > 0 {
		if len(p) >= b.tailLimit {
			b.tail = append(b.tail[:0], p[len(p)-b.tailLimit:]...)
		} else {
			overflow := len(b.tail) + len(p) - b.tailLimit
			if overflow > 0 {
				copy(b.tail, b.tail[overflow:])
				b.tail = b.tail[:len(b.tail)-overflow]
			}
			b.tail = append(b.tail, p...)
		}
	}
	return written, nil
}

func (b *boundedHeadTailBuffer) Bytes() []byte {
	if b.total <= int64(len(b.head)) {
		return append([]byte(nil), b.head...)
	}
	retained := len(b.head) + len(b.tail)
	if b.total <= int64(retained) {
		overlap := retained - int(b.total)
		result := make([]byte, 0, int(b.total))
		result = append(result, b.head...)
		return append(result, b.tail[overlap:]...)
	}
	result := make([]byte, 0, retained+len(boundedGapMarker))
	result = append(result, b.head...)
	result = append(result, boundedGapMarker...)
	return append(result, b.tail...)
}

func (b *boundedHeadTailBuffer) Truncated() bool {
	return b.total > int64(len(b.head)+len(b.tail))
}

func (b *boundedHeadTailBuffer) truncationWarning(source string) string {
	return fmt.Sprintf("%s exceeded the %d-byte capture limit; retained bounded head and final tail", source, b.headLimit+b.tailLimit)
}

func readBoundedHeadTailFile(path string, headLimit int, tailLimit int) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	limit := headLimit + tailLimit
	if info.Size() <= int64(limit) {
		data := make([]byte, int(info.Size()))
		_, err := io.ReadFull(file, data)
		if err != nil && err != io.ErrUnexpectedEOF {
			return nil, false, err
		}
		return data, false, nil
	}
	head := make([]byte, headLimit)
	if _, err := io.ReadFull(file, head); err != nil {
		return nil, false, err
	}
	if _, err := file.Seek(-int64(tailLimit), io.SeekEnd); err != nil {
		return nil, false, err
	}
	tail := make([]byte, tailLimit)
	if _, err := io.ReadFull(file, tail); err != nil {
		return nil, false, err
	}
	data := make([]byte, 0, limit+len(boundedGapMarker))
	data = append(data, head...)
	data = append(data, boundedGapMarker...)
	return append(data, tail...), true, nil
}

func extractFinalTranscriptText(path string, startOffset int64) (string, []string) {
	data, truncated, err := readTranscriptTail(path, startOffset)
	warnings := []string{}
	if err != nil {
		if !os.IsNotExist(err) {
			warnings = append(warnings, "agy transcript read failed: "+compactFileError(err))
		}
		return "", warnings
	}
	if truncated {
		warnings = append(warnings, fmt.Sprintf("agy transcript growth exceeded the %d-byte read limit; retained final tail", agyTranscriptReadLimitBytes))
	}
	text := ""
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 64<<10), agyTranscriptLineLimitBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var item map[string]any
		if err := json.Unmarshal([]byte(line), &item); err != nil {
			continue
		}
		if item["source"] != "MODEL" || item["type"] != "PLANNER_RESPONSE" {
			continue
		}
		if calls, ok := item["tool_calls"].([]any); ok && len(calls) > 0 {
			continue
		}
		if content, ok := item["content"].(string); ok && strings.TrimSpace(content) != "" {
			text = strings.TrimSpace(content)
		}
	}
	if err := scanner.Err(); err != nil {
		warnings = append(warnings, "agy transcript JSONL token exceeded the bounded scanner limit")
	}
	return text, warnings
}

func readTranscriptTail(path string, startOffset int64) ([]byte, bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, false, err
	}
	if startOffset < 0 {
		startOffset = 0
	}
	if info.Size() <= startOffset {
		return []byte{}, false, nil
	}
	readStart := startOffset
	truncated := info.Size()-startOffset > agyTranscriptReadLimitBytes
	if truncated {
		readStart = info.Size() - agyTranscriptReadLimitBytes
	}
	if _, err := file.Seek(readStart, io.SeekStart); err != nil {
		return nil, false, err
	}
	data := make([]byte, int(info.Size()-readStart))
	if _, err := io.ReadFull(file, data); err != nil && err != io.ErrUnexpectedEOF {
		return nil, false, err
	}
	if truncated {
		if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
			data = data[newline+1:]
		} else {
			data = nil
		}
	}
	return data, truncated, nil
}
