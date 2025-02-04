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
	context context.Context
	maxRead int

	buffer []byte
	cursor int // Points to beginning of next json object
	length int // Number of bytes used in buffer

	// Parsing policy
	maxDepth        uint8
	maxStringLength uint16
	maxArrayLength  uint16
	maxObjectLength uint16
}

// Create a new JsonStreamLexer with the given reader and buffer size.
func NewJsonStreamLexer(
	context context.Context,
	reader io.Reader,
	bufferSize int,
	maxRead int,
) *JsonStreamLexer {
	buffer := make([]byte, bufferSize)

	return &JsonStreamLexer{
		reader:  reader,
		context: context,
		buffer:  buffer,
		maxRead: maxRead,

		maxDepth:        20,
		maxStringLength: 9999,
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

func (l *JsonStreamLexer) DecodeAll(cb func([]byte), errCb func(error)) {
	for {
		select {
		case <-l.context.Done():
			return
		default:
			n, err := l.Read()

			if err == io.EOF {
				l.processBuffer(cb, errCb)
				return
			}

			// Exit on real errors
			if err != nil && err != io.ErrUnexpectedEOF {
				errCb(err)
				return
			}

			if n == 0 && err == io.ErrUnexpectedEOF {
				continue // Try reading again if we need more data
			}

			// Process available objects and continue if we need more data
			if complete := l.processBuffer(cb, errCb); complete {
				return
			}
		}
	}
}

type parseState struct {
	objectDepth  uint8
	arrayDepth   uint8
	stringLength uint16
	arrayLength  uint16
	objectLength uint16
	maxDepth     uint8
	maxArrayLen  uint16
	maxObjectLen uint16
	maxStringLen uint16
}

func (l *JsonStreamLexer) NextObject() (start, end int, err error) {
	var state uint8
	ps := parseState{
		maxDepth:     l.maxDepth,
		maxArrayLen:  l.maxArrayLength,
		maxObjectLen: l.maxObjectLength,
		maxStringLen: l.maxStringLength,
	}

	// Find start of object/array
	buf := l.buffer[l.cursor:l.length]
	offset, err := skipWhitespace(buf)
	if err != nil {
		return 0, 0, err
	}
	if offset == len(buf) {
		return l.cursor, -1, nil
	}

	start = l.cursor + offset
	if buf[offset] != '{' && buf[offset] != '[' {
		return 0, 0, fmt.Errorf(
			"invalid JSON: unexpected character '%c' at position %d",
			buf[offset],
			start,
		)
	}

	buf = l.buffer[start:l.length]
	for i := 0; i < len(buf); {
		if state&stateInString != 0 {
			endOffset, escaped, err := scanString(buf[i:], ps.maxStringLen)
			if err != nil {
				return 0, 0, err
			}
			if endOffset == -1 {
				return start, -1, nil // Need more data
			}
			i += endOffset
			if escaped {
				state |= stateEscaped
			} else {
				state &^= stateInString
			}
			continue
		}

		structOffset, char := findStructuralChar(buf[i:])
		if structOffset == -1 {
			return start, -1, nil // Need more data
		}
		i += structOffset

		switch char {
		case '"':
			state |= stateInString
		case '{':
			ps.objectDepth++
			if ps.objectDepth > ps.maxDepth {
				return 0, 0, fmt.Errorf("object exceeds maximum depth of %d", ps.maxDepth)
			}
			if ps.objectDepth == 1 && ps.arrayDepth == 0 {
				ps.objectLength++
				if ps.objectLength > ps.maxObjectLen {
					return 0, 0, fmt.Errorf("object count exceeds maximum of %d", ps.maxObjectLen)
				}
			}
		case '[':
			ps.arrayDepth++
			if ps.arrayDepth > ps.maxDepth {
				return 0, 0, fmt.Errorf("array exceeds maximum depth of %d", ps.maxDepth)
			}
			ps.arrayLength++
			if ps.arrayLength > ps.maxArrayLen {
				return 0, 0, fmt.Errorf("array length exceeds maximum of %d", ps.maxArrayLen)
			}
		case '}':
			if ps.objectDepth == 0 {
				return 0, 0, fmt.Errorf(
					"invalid JSON: unmatched closing bracket at position %d",
					start+i,
				)
			}
			ps.objectDepth--
			if ps.objectDepth == 0 && ps.arrayDepth == 0 {
				return start, start + i, nil
			}
		case ']':
			if ps.arrayDepth == 0 {
				return 0, 0, fmt.Errorf(
					"invalid JSON: unmatched closing bracket at position %d",
					start+i,
				)
			}
			ps.arrayDepth--
			if ps.objectDepth == 0 && ps.arrayDepth == 0 {
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
			return true // Exit on parsing errors
		}
		if end == -1 {
			return false // Need more data
		}

		cb(l.buffer[start : end+1])
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
