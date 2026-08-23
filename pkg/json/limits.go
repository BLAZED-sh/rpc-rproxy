package json

// Limits bound what a JsonStreamLexer will accept from a stream. A zero field
// means that limit is not enforced.
//
// These are framing limits, not policy. The lexer's job is to find object
// boundaries in a stream; deciding what a caller is allowed to send belongs to
// the caller. They exist so that a hostile or broken peer cannot make the lexer
// buffer without bound.
//
// Direction matters. A tenant sending requests is untrusted and should be
// bounded. A node answering eth_getLogs or trace_replayBlockTransactions can
// legitimately return tens of megabytes across hundreds of thousands of arrays,
// so response-side limits have to be generous or absent. Use StrictLimits for
// the first and DefaultLimits for the second.
type Limits struct {
	// MaxDepth caps nesting of objects and arrays. debug_* and trace_* results
	// nest far deeper than ordinary RPC payloads.
	MaxDepth int

	// MaxStringLength caps a single JSON string token in bytes.
	MaxStringLength int

	// MaxArrayCount caps how many '[' tokens may appear within one top-level
	// object. Note this counts arrays, not elements: an eth_getLogs response
	// opens one array per log entry for "topics", so this tracks roughly the
	// number of results rather than their size.
	MaxArrayCount int

	// MaxObjectCount caps how many '{' tokens may appear at depth one outside
	// any array within one top-level object.
	MaxObjectCount int

	// MaxObjectSize caps a single top-level JSON object in bytes. This is the
	// limit that actually bounds memory, since the lexer must buffer a whole
	// object before it can hand it to a callback.
	MaxObjectSize int
}

// DefaultLimits is permissive enough for real Ethereum JSON-RPC responses while
// still bounding memory. Nothing here should trip on a legitimate payload.
//
// It deliberately does not cap array or object counts. The previous fixed cap of
// 9999 arrays rejected any eth_getLogs response holding more than about 9999
// entries, and because it counted '[' tokens the failure surfaced as a parse
// error that dropped the connection rather than as anything a caller could
// diagnose.
func DefaultLimits() Limits {
	return Limits{
		MaxDepth:        256,
		MaxStringLength: 64 << 20,  // 64 MiB
		MaxArrayCount:   0,         // unlimited
		MaxObjectCount:  0,         // unlimited
		MaxObjectSize:   512 << 20, // 512 MiB
	}
}

// StrictLimits suits untrusted input, such as requests arriving from a tenant.
// Requests are small by nature: even a large batch of calls with generous
// parameters stays far inside these bounds.
func StrictLimits() Limits {
	return Limits{
		MaxDepth:        64,
		MaxStringLength: 1 << 20, // 1 MiB
		MaxArrayCount:   100_000,
		MaxObjectCount:  100_000,
		MaxObjectSize:   32 << 20, // 32 MiB
	}
}

// Unlimited disables every limit. Only appropriate when the peer is trusted and
// memory is bounded some other way.
func Unlimited() Limits {
	return Limits{}
}
