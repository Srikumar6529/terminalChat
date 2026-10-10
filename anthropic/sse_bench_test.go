package anthropic

import (
	"io"
	"strings"
	"testing"
)

func BenchmarkReadSSE_Small(b *testing.B) {
	body := sampleSSEStream(5)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text, err := readSSE(strings.NewReader(body), nil)
		if err != nil || text != "xxxxx" {
			b.Fatalf("text=%q err=%v", text, err)
		}
	}
}

func BenchmarkReadSSE_Large(b *testing.B) {
	body := sampleSSEStream(1000)
	want := strings.Repeat("x", 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text, err := readSSE(strings.NewReader(body), nil)
		if err != nil || text != want {
			b.Fatalf("len=%d err=%v", len(text), err)
		}
	}
}

func BenchmarkReadSSE_Callback(b *testing.B) {
	body := sampleSSEStream(100)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		n := 0
		_, err := readSSE(strings.NewReader(body), func(s string) error {
			n += len(s)
			return nil
		})
		if err != nil || n != 100 {
			b.Fatalf("n=%d err=%v", n, err)
		}
	}
}

// chunkReader delivers data in fixed-size chunks to stress scanner boundaries.
type chunkReader struct {
	data string
	size int
	pos  int
}

func (r *chunkReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	end := r.pos + r.size
	if end > len(r.data) {
		end = len(r.data)
	}
	n := copy(p, r.data[r.pos:end])
	r.pos += n
	return n, nil
}

func BenchmarkReadSSE_ChunkedCRLF(b *testing.B) {
	body := strings.ReplaceAll(sampleSSEStream(50), "\n", "\r\n")
	want := strings.Repeat("x", 50)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		text, err := readSSE(&chunkReader{data: body, size: 17}, nil)
		if err != nil || text != want {
			b.Fatalf("text=%q err=%v", text, err)
		}
	}
}
