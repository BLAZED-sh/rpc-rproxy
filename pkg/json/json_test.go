package json

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestNextObject(t *testing.T) {
	testCases := []struct {
		name  string
		input string

		startExpect int
		endExpect   int
	}{
		{
			name:        "two objects",
			input:       `{"key1": "value1"}{"key2": "value2"}`,
			startExpect: 0,
			endExpect:   17,
		},
		{
			name:        "one object",
			input:       `{"key": "value"}`,
			startExpect: 0,
			endExpect:   15,
		},
		{
			name:        "white spaces + one object",
			input:       `   {"key": "value"}`,
			startExpect: 3,
			endExpect:   18,
		},
		{
			name:        "unfinished object",
			input:       `{"unfinished": "object"`,
			startExpect: 0,
			endExpect:   -1,
		},
		{
			name:        "unfinished array",
			input:       `[1,2,3`,
			startExpect: 0,
			endExpect:   -1,
		},
		{
			name:        "complete array with mixed types",
			input:       `[1, "string", true, {"key": "value"}, [2,3]]`,
			startExpect: 0,
			endExpect:   43,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader([]byte(tc.input))
			lexer := NewJsonStreamLexer(reader, 16384, 4096, false)
			_, _ = lexer.Read()

			start, end, err := lexer.NextObject()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if start != tc.startExpect {
				t.Errorf("expected start to be %d, got %d", tc.startExpect, start)
			}

			if end != tc.endExpect {
				t.Errorf("expected end to be %d, got %d", tc.endExpect, end)
			}
		})
	}
}

func TestNextObjectErrors(t *testing.T) {
	testCases := []struct {
		name     string
		input    string
		maxDepth uint8
		wantErr  string
	}{
		{
			name:     "unmatched closing brace",
			input:    "}",
			maxDepth: 10,
			wantErr:  "invalid JSON: unmatched closing bracket at position 0",
		},
		{
			name:     "unmatched closing array",
			input:    `{"foo": [1,2]]}`,
			maxDepth: 10,
			wantErr:  "invalid JSON: unmatched closing bracket at position 13",
		},
		{
			name:     "exceeds max depth - nested objects",
			input:    `{"a": {"b": {"c": {"d": 1}}}}`,
			maxDepth: 2,
			wantErr:  "object exceeds maximum depth of 2",
		},
		{
			name:     "exceeds max depth - nested arrays",
			input:    `[[[["too deep"]]]]`,
			maxDepth: 2,
			wantErr:  "array exceeds maximum depth of 2",
		},
		{
			name:     "invalid character before object",
			input:    `x{"foo": "bar"}`,
			maxDepth: 10,
			wantErr:  "invalid JSON: unexpected character 'x' at position 0",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			reader := bytes.NewReader([]byte(tc.input))
			lexer := NewJsonStreamLexer(reader, 16384, 4096, false)
			lexer.maxDepth = tc.maxDepth
			_, _ = lexer.Read()

			_, _, err := lexer.NextObject()
			if err == nil {
				t.Fatal("expected an error but got nil")
			}

			if err.Error() != tc.wantErr {
				t.Errorf("expected error %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestDecodeAll(t *testing.T) {
	expected := []string{
		`{"key1": "value1"}`,
		`{"key2": "value2"}`,
		`{"key3": "value3"}`,
	}
	input := expected[0] + expected[1] + expected[2]
	reader := bytes.NewReader([]byte(input))
	lexer := NewJsonStreamLexer(reader, 16384, 4096, false)

	count := 0
	lexer.DecodeAll(context.Background(), func(b []byte) {
		if string(b) != expected[count] {
			t.Errorf("object %d: expected %q, got %q", count+1, expected[count], string(b))
		}
		count++
	}, func(err error) {
		t.Fatalf("unexpected error: %v", err)
	})

	if count > len(expected) {
		t.Fatalf("received more objects than expected")
	}
}

func TestDecodeAllBig(t *testing.T) {
	// Create a large nested JSON object
	var builder strings.Builder
	builder.WriteString(`{"root": {`)

	// Create 100 nested objects with long string values to exceed 512 bytes
	for i := 0; i < 100; i++ {
		if i > 0 {
			builder.WriteString(",")
		}
		fmt.Fprintf(&builder, `"key%d": {"name": "%s", "value": "%s"}`,
			i,
			strings.Repeat("very long name ", 10),
			strings.Repeat("very long value ", 20),
		)
	}
	builder.WriteString("}}")

	input := builder.String()
	if len(input) < 4096*2 {
		t.Fatalf("test input too small: %d bytes", len(input))
	}

	reader := bytes.NewReader([]byte(input))
	lexer := NewJsonStreamLexer(reader, 16384, 4096, false)

	lexer.DecodeAll(context.Background(), func(b []byte) {
		// TODO: check data
	}, func(err error) {
		t.Fatalf("unexpected error: %v", err)
	})
}

func BenchmarkDecodeAll(b *testing.B) {
	benchmarks := []struct {
		name      string
		generator func() string
		size      int
	}{
		{
			name: "small objects",
			generator: func() string {
				return `{"id": 1, "name": "test", "value": 123.45}`
			},
			size: 700,
		},
		{
			name: "medium array",
			generator: func() string {
				var b strings.Builder
				b.WriteString("[")
				for i := 0; i < 1000; i++ {
					if i > 0 {
						b.WriteString(",")
					}
					fmt.Fprintf(&b, `{"id":%d,"value":"test-%d"}`, i, i)
				}
				b.WriteString("]")
				return b.String()
			},
			size: 1,
		},
		{
			name: "large nested objects",
			generator: func() string {
				var b strings.Builder
				b.WriteString(`{"root":{"items":[`)
				for i := 0; i < 500; i++ {
					if i > 0 {
						b.WriteString(",")
					}
					fmt.Fprintf(&b, `{"id":%d,"data":{"name":"item-%d","values":[%d,%d,%d]}}`,
						i, i, i*2, i*3, i*4)
				}
				b.WriteString(`]}}`)
				return b.String()
			},
			size: 1,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			// Generate input data once before the benchmark
			var fullInput strings.Builder
			for i := 0; i < bm.size; i++ {
				fullInput.WriteString(bm.generator())
			}
			input := fullInput.String()
			inputSize := len(input)

			// Set the bytes count for throughput calculation
			b.SetBytes(int64(inputSize))

			// Reset the timer before the actual benchmark
			b.ResetTimer()

			// Run the benchmark N times
			for i := 0; i < b.N; i++ {
				b.StopTimer()
				reader := bytes.NewReader([]byte(input))
				b.StartTimer()

				lexer := NewJsonStreamLexer(reader, 32768, 16384, false)

				var totalBytes int
				lexer.DecodeAll(context.Background(), func(data []byte) {
					totalBytes += len(data)
				}, func(err error) {
					b.Fatalf("unexpected error: %v", err)
				})
				if totalBytes != inputSize {
					b.Fatalf("not all data was processed: expected %d bytes, got %d bytes",
						inputSize, totalBytes)
				}
			}
		})
	}
}
