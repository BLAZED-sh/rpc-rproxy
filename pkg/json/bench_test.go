package json

import (
	"bytes"
	"context"
	"testing"
)

func benchLogs(b *testing.B, n int) {
	input := []byte(logsResponse(n))
	b.SetBytes(int64(len(input)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		lexer := NewJsonStreamLexerWithLimits(bytes.NewReader(input), 4096, 4096, false, DefaultLimits())
		lexer.DecodeAll(context.Background(), func([]byte) {}, func(error) {})
	}
}

func BenchmarkLogs1k(b *testing.B)  { benchLogs(b, 1_000) }
func BenchmarkLogs10k(b *testing.B) { benchLogs(b, 10_000) }
func BenchmarkLogs40k(b *testing.B) { benchLogs(b, 40_000) }
