//go:build !amd64

package json

import "fmt"

func findStructuralChar(buf []byte) (offset int, char byte) {
	for i, c := range buf {
		if isStructural[c] {
			return i, c
		}
	}
	return -1, 0
}

func skipWhitespace(buf []byte) (offset int, err error) {
	for i, c := range buf {
		if !isWhitespace[c] {
			return i, nil
		}
	}
	return len(buf), nil
}

func scanString(buf []byte, maxLen uint16) (endOffset int, escaped bool, err error) {
	var length uint16
	for i, c := range buf {
		length++
		if length > maxLen {
			return 0, false, fmt.Errorf("string exceeds maximum length of %d", maxLen)
		}

		if escaped {
			return i + 1, false, nil
		}

		if c == '\\' {
			return i + 1, true, nil
		}

		if c == '"' {
			return i + 1, false, nil
		}
	}
	return -1, false, nil
}
