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

	// Parsing policy
	maxDepth        uint8
	maxStringLength uint32
	maxArrayLength  uint16
	maxObjectLength uint16
}

// Create a new JsonStreamLexer with the given reader and buffer size.
func NewJsonStreamLexer(
	reader io.Reader,
	bufferSize int,
	maxRead int,
	asyncCallbacks bool,
) *JsonStreamLexer {
	buffer := make([]byte, bufferSize)

	return &JsonStreamLexer{
		reader:  reader,
		buffer:  buffer,
		maxRead: maxRead,

		asyncCallbacks: asyncCallbacks,

		maxDepth:        20,
		maxStringLength: 999999,
		maxArrayLength:  9999,
		maxObjectLength: 9999,
	}
}

func (l *JsonStreamLexer) Read() (int, error) {
	// Ensure we have room for at least maxRead more data
	bCap := cap(l.buffer)
	remainingCap := bCap - l.length
	minCap := l.length + l.maxRead
	if remainingCap < minCap {
		var newCap int
		if bCap < minCap {
			newCap = minCap
		} else {
			newCap = bCap * 2
		}
		newBuffer := make([]byte, newCap)
		copy(newBuffer, l.buffer)
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
	for {
		select {
		case <-context.Done():
		default:
			if l.length > 0 && lastObjComplete {
				lastObjComplete = l.processBuffer(cb, errCb)
			}

			_, err := l.Read()

			if err == io.EOF {
				l.processBuffer(cb, errCb)
				return
			}

			// Exit on real errors
			if err != nil && err != io.ErrUnexpectedEOF {
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

func (l *JsonStreamLexer) NextObject() (start, end int, err error) {
	const (
		stateInString = 1 << iota
		stateEscaped
	)
	var state uint8

	// Use uint8 for depths since JSON rarely nests deeply
	var (
		objectDepth  uint8
		arrayDepth   uint8
		stringLength uint32
		arrayLength  uint16
		objectLength uint16
	)

	// Find start of object/array
	buf := l.buffer[l.cursor:l.length]
	for i := 0; i < len(buf); i++ {
		c := buf[i]
		if c == '{' || c == '[' {
			start = l.cursor + i
			goto parseLoop
		}
		if c == '}' || c == ']' {
			return 0, 0, fmt.Errorf(
				"invalid JSON: unmatched closing bracket at position %d",
				l.cursor+i,
			)
		}
		if !isWhitespace[c] {
			return 0, 0, fmt.Errorf(
				"invalid JSON: unexpected character '%c' at position %d",
				c,
				l.cursor+i,
			)
		}
	}
	return l.cursor, -1, nil

parseLoop:
	buf = l.buffer[start:l.length]
	for i := 0; i < len(buf); i++ {
		c := buf[i]

		if state&stateInString != 0 {
			stringLength++
			if stringLength > l.maxStringLength {
				return 0, 0, fmt.Errorf("string exceeds maximum length of %d", l.maxStringLength)
			}

			if state&stateEscaped != 0 {
				state &^= stateEscaped
				continue
			}

			if c == '\\' {
				state |= stateEscaped
				continue
			}
			if c == '"' {
				state &^= stateInString
				stringLength = 0
			}
			continue
		}

		// Fast path for non-structural characters
		if !isStructural[c] {
			continue
		}

		switch c {
		case '"':
			state |= stateInString
		case '{':
			objectDepth++
			if objectDepth > l.maxDepth {
				return 0, 0, fmt.Errorf("object exceeds maximum depth of %d", l.maxDepth)
			}

			if objectDepth == 1 && arrayDepth == 0 {
				objectLength++
				if objectLength > l.maxObjectLength {
					return 0, 0, fmt.Errorf("object count exceeds maximum of %d", l.maxObjectLength)
				}
			}
		case '[':
			arrayDepth++
			if arrayDepth > l.maxDepth {
				return 0, 0, fmt.Errorf("array exceeds maximum depth of %d", l.maxDepth)
			}
			arrayLength++
			if arrayLength > l.maxArrayLength {
				return 0, 0, fmt.Errorf("array length exceeds maximum of %d", l.maxArrayLength)
			}
		case '}':
			if objectDepth == 0 {
				return 0, 0, fmt.Errorf(
					"invalid JSON: unmatched closing bracket at position %d",
					start+i,
				)
			}
			objectDepth--
			if objectDepth == 0 && arrayDepth == 0 {
				return start, start + i, nil
			}
		case ']':
			if arrayDepth == 0 {
				return 0, 0, fmt.Errorf(
					"invalid JSON: unmatched closing bracket at position %d",
					start+i,
				)
			}
			arrayDepth--
			if objectDepth == 0 && arrayDepth == 0 {
				return start, start + i, nil
			}
		}
	}

	return start, -1, nil
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
