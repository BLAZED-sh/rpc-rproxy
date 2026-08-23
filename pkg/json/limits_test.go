package json

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// decodeAll runs the lexer over input and returns the framed objects.
func decodeAll(t *testing.T, input string, limits Limits) ([]string, []error) {
	t.Helper()

	lexer := NewJsonStreamLexerWithLimits(bytes.NewReader([]byte(input)), 4096, 4096, false, limits)

	var got []string
	var errs []error
	lexer.DecodeAll(
		context.Background(),
		func(b []byte) { got = append(got, string(b)) },
		func(err error) { errs = append(errs, err) },
	)
	return got, errs
}

// logsResponse builds an eth_getLogs reply with n entries. Each entry carries a
// "topics" array, so n entries mean n+1 '[' tokens.
func logsResponse(n int) string {
	var sb strings.Builder
	sb.WriteString(`{"jsonrpc":"2.0","id":1,"result":[`)
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(
			&sb,
			`{"address":"0x7a250d5630b4cf539739df2c5dacb4c659f2488d","blockNumber":"0x%x","topics":["0xddf252ad1be2c89b69c2b068fc378daa952ba7f163c4a11628f55a4df523b3ef"]}`,
			i,
		)
	}
	sb.WriteString(`]}`)
	return sb.String()
}

// batchRequest builds a JSON-RPC batch of n calls. Each element carries a
// "params" array.
func batchRequest(n int) string {
	var sb strings.Builder
	sb.WriteByte('[')
	for i := 0; i < n; i++ {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(
			&sb,
			`{"jsonrpc":"2.0","id":%d,"method":"eth_getBalance","params":["0x742d35cc6634c0532925a3b844bc454e4438f44e","latest"]}`,
			i,
		)
	}
	sb.WriteByte(']')
	return sb.String()
}

// A response larger than the old fixed cap of 9999 arrays used to fail to parse
// and take the connection with it. Under DefaultLimits it must come through.
func TestDefaultLimitsAcceptLargeLogsResponse(t *testing.T) {
	for _, n := range []int{9_000, 10_001, 50_000} {
		t.Run(fmt.Sprintf("%d logs", n), func(t *testing.T) {
			input := logsResponse(n)

			got, errs := decodeAll(t, input, DefaultLimits())
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 framed object, got %d", len(got))
			}
			if got[0] != input {
				t.Errorf("framed object differs from input (%d vs %d bytes)", len(got[0]), len(input))
			}
		})
	}
}

// Batched calls arrive as a single top-level array and must be framed as one
// object, not split and not dropped.
func TestBatchedRequestsFrameAsOneObject(t *testing.T) {
	for _, n := range []int{1, 100, 20_000} {
		t.Run(fmt.Sprintf("%d calls", n), func(t *testing.T) {
			input := batchRequest(n)

			got, errs := decodeAll(t, input, StrictLimits())
			if len(errs) > 0 {
				t.Fatalf("unexpected errors: %v", errs)
			}
			if len(got) != 1 {
				t.Fatalf("expected 1 framed object for a batch, got %d", len(got))
			}
			if got[0] != input {
				t.Errorf("framed batch differs from input (%d vs %d bytes)", len(got[0]), len(input))
			}
		})
	}
}

// Batches and single calls pipelined on one stream must frame independently.
func TestMixedBatchAndSingleStream(t *testing.T) {
	single := `{"jsonrpc":"2.0","id":1,"method":"eth_blockNumber","params":[]}`
	batch := batchRequest(3)
	input := single + "\n" + batch + "\n" + single + "\n"

	got, errs := decodeAll(t, input, StrictLimits())
	if len(errs) > 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	want := []string{single, batch, single}
	if len(got) != len(want) {
		t.Fatalf("expected %d framed objects, got %d", len(want), len(got))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("object %d: got %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMaxObjectSizeBoundsMemory(t *testing.T) {
	limits := Unlimited()
	limits.MaxObjectSize = 4096

	input := logsResponse(500) // comfortably over 4KiB
	got, errs := decodeAll(t, input, limits)

	if len(got) != 0 {
		t.Errorf("expected the oversized object to be rejected, got %d objects", len(got))
	}
	if len(errs) == 0 {
		t.Fatal("expected an error for an object over MaxObjectSize")
	}
	if !strings.Contains(errs[0].Error(), "maximum size") {
		t.Errorf("expected a size error, got %v", errs[0])
	}
}

// The counters used to be uint16, so a payload with more than 65535 arrays
// wrapped the counter back below the cap and slipped past the check entirely.
func TestArrayCountDoesNotWrap(t *testing.T) {
	limits := Unlimited()
	limits.MaxArrayCount = 9999

	// 70_000 topics arrays: above uint16 range, so a wrapping counter would
	// land near 4464 and wrongly report success.
	input := logsResponse(70_000)
	got, errs := decodeAll(t, input, limits)

	if len(got) != 0 {
		t.Errorf("expected rejection above MaxArrayCount, got %d objects", len(got))
	}
	if len(errs) == 0 {
		t.Fatal("expected an array count error; a wrapping counter would silently accept this")
	}
	if !strings.Contains(errs[0].Error(), "array count") {
		t.Errorf("expected an array count error, got %v", errs[0])
	}
}

func TestZeroLimitMeansUnlimited(t *testing.T) {
	limits := Unlimited()

	input := logsResponse(20_000)
	got, errs := decodeAll(t, input, limits)

	if len(errs) > 0 {
		t.Fatalf("unexpected errors with all limits disabled: %v", errs)
	}
	if len(got) != 1 || got[0] != input {
		t.Errorf("expected the payload through untouched, got %d objects", len(got))
	}
}

// DecodeAll must stop when its context is cancelled. This used to be a bare
// break inside a select, which left the select and spun the loop.
func TestDecodeAllStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	lexer := NewJsonStreamLexerWithLimits(
		strings.NewReader(strings.Repeat(`{"a":1}`+"\n", 1000)),
		4096, 4096, false, DefaultLimits(),
	)

	done := make(chan struct{})
	go func() {
		lexer.DecodeAll(ctx, func([]byte) {}, func(error) {})
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("DecodeAll did not return on a cancelled context")
	}
}
