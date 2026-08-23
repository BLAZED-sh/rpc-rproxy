package json

import (
	"context"
	"fmt"
	"io"
)

// JsonStreamLexer is a streaming JSON lexer/seperator that reads JSON objects and arrays from an io.Reader.
// It is designed to be used in a streaming context where the input is a continuous stream of JSON objects
// like a JSONL file or a JSON RPC connection.
// This 'lexer' is actually more of a JSON object seperator that keeps track of the start and end of objects and arrays
// to split the input stream into individual parts that can be parsed by a real JSON decoder.
type JsonStreamLexer struct {
	reader  io.Reader
	maxRead int

	buffer []byte
	cursor int // Points to beginning of next json object
	length int // Number of bytes used in buffer

	asyncCallbacks bool

	// Parsing policy. See Limits; a zero field means unenforced.
	limits Limits

	// Progress through the object currently being framed. See scanState.
	scan scanState
}

// NewJsonStreamLexer creates a lexer using DefaultLimits, which is permissive
// enough for real Ethereum JSON-RPC responses. Use NewJsonStreamLexerWithLimits
// to bound untrusted input.
func NewJsonStreamLexer(
	reader io.Reader,
	bufferSize int,
	maxRead int,
	asyncCallbacks bool,
) *JsonStreamLexer {
	return NewJsonStreamLexerWithLimits(reader, bufferSize, maxRead, asyncCallbacks, DefaultLimits())
}

// NewJsonStreamLexerWithLimits creates a lexer with explicit parsing limits.
func NewJsonStreamLexerWithLimits(
	reader io.Reader,
	bufferSize int,
	maxRead int,
	asyncCallbacks bool,
	limits Limits,
) *JsonStreamLexer {
	buffer := make([]byte, bufferSize)

	return &JsonStreamLexer{
		reader:  reader,
		buffer:  buffer,
		maxRead: maxRead,

		asyncCallbacks: asyncCallbacks,

		limits: limits,
	}
}

func (l *JsonStreamLexer) Read() (int, error) {
	// Refuse to buffer past the object size limit rather than growing until the
	// process dies. Without this, removing the array and object count caps would
	// leave nothing bounding memory for a single object.
	if l.limits.MaxObjectSize > 0 && l.length > l.limits.MaxObjectSize {
		return 0, fmt.Errorf("object exceeds maximum size of %d bytes", l.limits.MaxObjectSize)
	}

	// Ensure we have room for at least maxRead more data.
	//
	// The condition used to compare remaining capacity against length+maxRead,
	// which grows the buffer on almost every read once anything is buffered.
	// What we actually need is room for one more read.
	bCap := cap(l.buffer)
	remainingCap := bCap - l.length
	if remainingCap < l.maxRead {
		newCap := bCap * 2
		if minCap := l.length + l.maxRead; newCap < minCap {
			newCap = minCap
		}
		newBuffer := make([]byte, newCap)
		copy(newBuffer, l.buffer[:l.length])
		l.buffer = newBuffer
	}

	// Read into buffer
	n, err := l.reader.Read(l.buffer[l.length : l.length+l.maxRead])
	if err != nil {
		return n, err
	}
	l.length += n
	// Remove zeros from read
	l.buffer = l.buffer[:l.length]

	return n, nil
}

// Try to read the stream object by object till we hit EOF
func (l *JsonStreamLexer) DecodeAll(context context.Context, cb func([]byte), errCb func(error)) {
	lastObjComplete := true
	done := context.Done()
	for {
		select {
		case <-done:
			return
		default:
			if l.length > 0 && lastObjComplete {
				lastObjComplete = l.processBuffer(cb, errCb)
			}

			n, err := l.Read()
			if n == 0 && err == nil {
				continue // No new data, read again
			}

			if err == io.EOF {
				l.processBuffer(cb, errCb)
				return
			}

			// Exit on real errors
			if err != nil {
				errCb(err)
				return
			}

			// Reset lastObjComplete if we read new data successfully
			if !lastObjComplete {
				lastObjComplete = true
			}
		}
	}
}

// Pre-computed lookup tables for character classification
var (
	isWhitespace [256]bool
	isStructural [256]bool
)

func init() {
	// Initialize lookup tables
	isWhitespace[' '], isWhitespace['\n'], isWhitespace['\r'], isWhitespace['\t'] = true, true, true, true
	isStructural['{'], isStructural['}'], isStructural['['], isStructural[']'], isStructural['"'] = true, true, true, true, true
}

// scanState carries NextObject's progress between calls
type scanState struct {
	active       bool
	start        int
	pos          int
	state        uint8
	objectDepth  int
	arrayDepth   int
	stringLength int
	arrayCount   int
	objectCount  int
}

const (
	stateInString = 1 << iota
	stateEscaped
)

// NextObject returns the bounds of the next complete JSON value in the buffer.
// An end of -1 means the value is incomplete and more data is needed; progress
// is retained, so the next call resumes where this one stopped.
func (l *JsonStreamLexer) NextObject() (start, end int, err error) {
	s := &l.scan

	if !s.active {
		// Find the start of the next object or array.
		for i := l.cursor; i < l.length; i++ {
			c := l.buffer[i]
			if c == '{' || c == '[' {
				*s = scanState{active: true, start: i, pos: i}
				break
			}
			if c == '}' || c == ']' {
				return 0, 0, fmt.Errorf(
					"invalid JSON: unmatched closing bracket at position %d",
					i,
				)
			}
			if !isWhitespace[c] {
				return 0, 0, fmt.Errorf(
					"invalid JSON: unexpected character '%c' at position %d",
					c,
					i,
				)
			}
		}
		if !s.active {
			return l.cursor, -1, nil
		}
	}

	fail := func(format string, args ...any) (int, int, error) {
		s.active = false
		return 0, 0, fmt.Errorf(format, args...)
	}

	for ; s.pos < l.length; s.pos++ {
		c := l.buffer[s.pos]

		if l.limits.MaxObjectSize > 0 && s.pos-s.start > l.limits.MaxObjectSize {
			return fail("object exceeds maximum size of %d bytes", l.limits.MaxObjectSize)
		}

		if s.state&stateInString != 0 {
			s.stringLength++
			if l.limits.MaxStringLength > 0 && s.stringLength > l.limits.MaxStringLength {
				return fail("string exceeds maximum length of %d", l.limits.MaxStringLength)
			}

			if s.state&stateEscaped != 0 {
				s.state &^= stateEscaped
				continue
			}

			if c == '\\' {
				s.state |= stateEscaped
				continue
			}
			if c == '"' {
				s.state &^= stateInString
				s.stringLength = 0
			}
			continue
		}

		// Fast path for non-structural characters
		if !isStructural[c] {
			continue
		}

		switch c {
		case '"':
			s.state |= stateInString
		case '{':
			s.objectDepth++
			if l.limits.MaxDepth > 0 && s.objectDepth > l.limits.MaxDepth {
				return fail("object exceeds maximum depth of %d", l.limits.MaxDepth)
			}

			if s.objectDepth == 1 && s.arrayDepth == 0 {
				s.objectCount++
				if l.limits.MaxObjectCount > 0 && s.objectCount > l.limits.MaxObjectCount {
					return fail("object count exceeds maximum of %d", l.limits.MaxObjectCount)
				}
			}
		case '[':
			s.arrayDepth++
			if l.limits.MaxDepth > 0 && s.arrayDepth > l.limits.MaxDepth {
				return fail("array exceeds maximum depth of %d", l.limits.MaxDepth)
			}
			s.arrayCount++
			if l.limits.MaxArrayCount > 0 && s.arrayCount > l.limits.MaxArrayCount {
				return fail("array count exceeds maximum of %d", l.limits.MaxArrayCount)
			}
		case '}':
			if s.objectDepth == 0 {
				return fail("invalid JSON: unmatched closing bracket at position %d", s.pos)
			}
			s.objectDepth--
			if s.objectDepth == 0 && s.arrayDepth == 0 {
				st, en := s.start, s.pos
				*s = scanState{}
				return st, en, nil
			}
		case ']':
			if s.arrayDepth == 0 {
				return fail("invalid JSON: unmatched closing bracket at position %d", s.pos)
			}
			s.arrayDepth--
			if s.objectDepth == 0 && s.arrayDepth == 0 {
				st, en := s.start, s.pos
				*s = scanState{}
				return st, en, nil
			}
		}
	}

	return s.start, -1, nil
}

// processBuffer processes complete objects in the buffer and calls the callback for each
func (l *JsonStreamLexer) processBuffer(cb func([]byte), errCb func(err error)) (complete bool) {
	for l.length > 0 {
		start, end, err := l.NextObject()
		if err != nil {
			errCb(err)
			// TODO: on parsing errors we need to try to skip the invalid part and continue parsing
			return true // Exit on parsing errors
		}
		if end == -1 {
			return false // Need more data
		}

		if l.asyncCallbacks {
			// TODO: check if this is smart
			data := make([]byte, end-start+1)
			copy(data, l.buffer[start:end+1])
			go cb(data)
		} else {
			cb(l.buffer[start : end+1])
		}

		//cb(data)
		l.cursor = end + 1

		// Compact buffer after each object
		if l.cursor > 0 {
			copy(l.buffer, l.buffer[l.cursor:l.length])
			l.length -= l.cursor
			l.cursor = 0
		}
	}
	return true
}

// The following methods are used for debugging

// BufferLength returns the number of bytes used in the buffer
func (l *JsonStreamLexer) BufferLength() int {
	return l.length
}

// Cursor returns the current cursor position
func (l *JsonStreamLexer) Cursor() int {
	return l.cursor
}

// Buffer returns the current buffer
func (l *JsonStreamLexer) Buffer() []byte {
	return l.buffer
}

// BufferContent returns a string representation of the current buffer content
func (l *JsonStreamLexer) BufferContent() string {
	if l.length == 0 {
		return "<empty>"
	}
	// Only return up to 100 bytes to avoid huge logs
	if l.length > 100 {
		return fmt.Sprintf("%s... (%d more bytes)", string(l.buffer[:100]), l.length-100)
	}
	return string(l.buffer[:l.length])
}
