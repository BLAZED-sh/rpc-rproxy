package json

// Constants for parsing state - shared between Go and assembly
const (
	stateInString = 1 << iota
	stateEscaped
)

//go:noescape
func findStructuralChar(buf []byte) (offset int, char byte)

//go:noescape
func skipWhitespace(buf []byte) (offset int, err error)

//go:noescape
func scanString(buf []byte, maxLen uint16) (endOffset int, escaped bool, err error)
