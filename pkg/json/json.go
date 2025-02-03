package json

import (
	"context"
	"fmt"
	"io"
)

// TODO: Handle buffer overflow

// A JSON stream lexer that reads JSON objects from a stream and sends them to a channel.
// Using a small read buffer of 4k bytes + the current unfinished object
type JsonStreamLexer struct {
	reader  io.Reader
	context context.Context
	maxRead int

	// Internal buffer management
	buffer []byte
	cursor int // Points to beginning of next json object
	length int // Number of bytes used in buffer

	// Parsing policy
	maxDepth        int
	maxStringLength int
	maxArrayLength  int
	maxObjectLength int
}

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
	// // Compact buffer
	// if l.cursor > 0 {
	// 	copy(l.buffer, l.buffer[l.cursor:l.length+l.maxRead])
	// 	l.length -= l.cursor
	// 	l.cursor = 0
	// }

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

// func (l *JsonStreamLexer) DecodeAll(objects chan []byte, errorsC chan error) {
func (l *JsonStreamLexer) DecodeAll(cb func([]byte)) {
	for {
		select {
		case <-l.context.Done():
			return
		default:
			_, err := l.Read()
			if err != nil {
				if err != io.EOF && err != io.ErrUnexpectedEOF {
					// errorsC <- err
					// close(objects)
					// close(errorsC)
				}
				break
			}

			for {
				// Process objects
				start, end, err := l.NextObject()
				if err != nil {
					l.length = 0
					l.cursor = 0
					// errorsC <- err
					break
				}

				// Object is not finished -> read more and try again
				if end == -1 {
					break
				}

				// objects <- l.buffer[start : end+1]
				cb(l.buffer[start : end+1])

				if end+1 < l.length {
					l.cursor = end + 1
				}

			}

			// Remove processed object from buffer
			l.buffer = l.buffer[l.cursor:]
			l.length -= l.cursor
			l.cursor = 0
		}
	}
}

func (l *JsonStreamLexer) NextObject() (start, end int, err error) {
	objectDepth := 0
	arrayDepth := 0
	inString := false
	escaped := false
	stringLength := 0
	arrayLength := 0
	objectLength := 0

	// Find start of object/array
	for start = l.cursor; start < l.length; start++ {
		c := l.buffer[start]
		if c == '{' || c == '[' {
			break
		}
		if c == '}' || c == ']' {
			return 0, 0, fmt.Errorf("invalid JSON: unmatched closing bracket at position %d", start)
		}
		if c != ' ' && c != '\n' && c != 0 {
			return 0, 0, fmt.Errorf(
				"invalid JSON: unexpected character '%c' at position %d",
				c,
				start,
			)
		}
	}

	for i := start; i < l.length; i++ {
		c := l.buffer[i]

		if inString {
			stringLength++
			if stringLength > l.maxStringLength {
				return 0, 0, fmt.Errorf("string exceeds maximum length of %d", l.maxStringLength)
			}

			if escaped {
				escaped = false
				continue
			}

			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
				stringLength = 0
			}
			continue
		}

		switch c {
		case '"':
			inString = true
		case '{':
			objectDepth++
			if objectDepth > l.maxDepth {
				return 0, 0, fmt.Errorf("object exceeds maximum depth of %d", l.maxDepth)
			}
			// Only count root-level objects
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
			objectDepth--
			if objectDepth < 0 {
				return 0, 0, fmt.Errorf("invalid JSON: unmatched closing bracket at position %d", i)
			}
			if objectDepth == 0 && arrayDepth == 0 {
				return start, i, nil
			}
		case ']':
			arrayDepth--
			if arrayDepth < 0 {
				return 0, 0, fmt.Errorf("invalid JSON: unmatched closing bracket at position %d", i)
			}
			if objectDepth == 0 && arrayDepth == 0 {
				return start, i, nil
			}
		}
	}

	// Object is not complete
	return start, -1, nil
}
