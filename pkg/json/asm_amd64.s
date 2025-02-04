#include "textflag.h"

DATA structural_chars<>+0(SB)/8, $0x225D5B7B225C7B22 // "{\"[]{\"}\""
DATA structural_chars<>+8(SB)/8, $0x0000000000000000
GLOBL structural_chars<>(SB), RODATA, $16

DATA whitespace_chars<>+0(SB)/8, $0x0A0D090020202020 // space, tab, CR, LF
GLOBL whitespace_chars<>(SB), RODATA, $8

// func findStructuralChar(buf []byte) (offset int, char byte)
TEXT ·findStructuralChar(SB), NOSPLIT, $0-24
	MOVQ buf+0(FP), SI    // buf pointer
	MOVQ buf+8(FP), CX    // buf length
	XORQ R8, R8           // offset counter
	
	// Load structural character masks
	VMOVDQU structural_chars<>(SB), Y1
	
	// Process 32 bytes at a time
loop:
	CMPQ CX, $32
	JL   tail
	
	// Load 32 bytes from buffer
	VMOVDQU (SI), Y0
	
	// Compare against structural characters
	VPCMPEQB Y0, Y1, Y2
	
	// Convert matches to bitmask
	VPMOVMSKB Y2, R9
	
	// Check if we found any matches
	TESTL R9, R9
	JZ    next_block
	
	// Find first match
	TZCNTL R9, R10
	ADDQ   R8, R10
	MOVQ   R10, offset+16(FP)
	ADDQ   R10, SI
	MOVB   (SI), char+24(FP)
	SUBQ   R10, SI
	RET

next_block:
	ADDQ $32, SI
	ADDQ $32, R8
	SUBQ $32, CX
	JMP  loop

tail:
	TESTQ CX, CX
	JZ    not_found
	
	// Process remaining bytes one at a time
tail_loop:
	MOVB (SI), AL
	CMPB AL, $'{'
	JE   found_char
	CMPB AL, $'}'
	JE   found_char
	CMPB AL, $'['
	JE   found_char
	CMPB AL, $']'
	JE   found_char
	CMPB AL, $'"'
	JE   found_char
	
	INCQ SI
	INCQ R8
	DECQ CX
	JNZ  tail_loop
	
not_found:
	MOVQ $-1, offset+16(FP)
	MOVB $0, char+24(FP)
	RET
	
found_char:
	MOVQ R8, offset+16(FP)
	MOVB AL, char+24(FP)
	RET

// func skipWhitespace(buf []byte) (offset int, err error)
TEXT ·skipWhitespace(SB), NOSPLIT, $0-32
	MOVQ buf+0(FP), SI    // buf pointer
	MOVQ buf+8(FP), CX    // buf length
	XORQ R8, R8           // offset counter
	
	// Load whitespace mask
	VMOVDQU whitespace_chars<>(SB), Y1
	
	// Process 32 bytes at a time
loop_ws:
	CMPQ CX, $32
	JL   tail_ws
	
	// Load 32 bytes from buffer
	VMOVDQU (SI), Y0
	
	// Compare against whitespace
	VPCMPEQB Y0, Y1, Y2
	
	// Convert matches to bitmask
	VPMOVMSKB Y2, R9
	
	// Count leading whitespace
	LZCNTL R9, R10
	CMPL   R10, $32
	JE     next_block_ws
	
	// Found non-whitespace
	ADDQ R8, R10
	MOVQ R10, offset+16(FP)
	MOVQ $0, err+24(FP)
	RET

next_block_ws:
	ADDQ $32, SI
	ADDQ $32, R8
	SUBQ $32, CX
	JMP  loop_ws

tail_ws:
	TESTQ CX, CX
	JZ    all_whitespace
	
tail_loop_ws:
	MOVB (SI), AL
	CMPB AL, $0x20  // space
	JE   next_ws
	CMPB AL, $0x09  // tab
	JE   next_ws
	CMPB AL, $0x0A  // LF
	JE   next_ws
	CMPB AL, $0x0D  // CR
	JE   next_ws
	
	// Found non-whitespace
	MOVQ R8, offset+16(FP)
	MOVQ $0, err+24(FP)
	RET
	
next_ws:
	INCQ SI
	INCQ R8
	DECQ CX
	JNZ  tail_loop_ws
	
all_whitespace:
	MOVQ R8, offset+16(FP)
	MOVQ $0, err+24(FP)
	RET

// func scanString(buf []byte, maxLen uint16) (endOffset int, escaped bool, err error)
TEXT ·scanString(SB), NOSPLIT, $0-48
	MOVQ buf+0(FP), SI      // buf pointer
	MOVQ buf+8(FP), CX      // buf length
	MOVW maxLen+16(FP), DX  // max length
	XORQ R8, R8             // offset counter
	XORQ R9, R9             // string length counter
	
	// Create quote and escape masks
	VPCMPEQB Y0, Y0, Y0
	VPXOR    Y1, Y1, Y1

	// Load quote (34) and backslash (92) into XMM registers
	MOVQ $0x22, AX  // ASCII for quote
	MOVQ AX, X2
	VPBROADCASTB X2, Y2
	
	MOVQ $0x5C, AX  // ASCII for backslash
	MOVQ AX, X3
	VPBROADCASTB X3, Y3
	
loop_str:
	CMPQ CX, $32
	JL   tail_str
	
	// Load 32 bytes
	VMOVDQU (SI), Y4
	
	// Find quotes and escapes
	VPCMPEQB Y4, Y2, Y5  // quotes
	VPCMPEQB Y4, Y3, Y6  // escapes
	
	// Convert to bitmasks
	VPMOVMSKB Y5, R10  // quotes mask
	VPMOVMSKB Y6, R11  // escapes mask
	
	// Check for matches
	TESTL R10, R10
	JNZ   found_quote_or_escape
	TESTL R11, R11
	JNZ   found_quote_or_escape
	
	// No special chars, add to length
	ADDQ $32, R9
	CMPQ R9, DX
	JA   string_too_long
	
next_block_str:
	ADDQ $32, SI
	ADDQ $32, R8
	SUBQ $32, CX
	JMP  loop_str

found_quote_or_escape:
	// Find first special char
	MOVL R10, R12
	ORL  R11, R12
	TZCNTL R12, R13
	
	// Add to string length
	ADDQ R13, R9
	CMPQ R9, DX
	JA   string_too_long
	
	// Check if it's an escape
	BTRL R13, R11
	JC   handle_escape
	
	// It's a quote - we're done
	ADDQ R13, R8
	INCQ R8  // include the quote
	MOVQ R8, endOffset+24(FP)
	MOVB $0, escaped+32(FP)
	MOVQ $0, err+40(FP)
	RET

handle_escape:
	ADDQ R13, R8
	INCQ R8  // move past escape char
	MOVQ R8, endOffset+24(FP)
	MOVB $1, escaped+32(FP)
	MOVQ $0, err+40(FP)
	RET

tail_str:
	TESTQ CX, CX
	JZ    need_more_data
	
tail_loop_str:
	MOVB (SI), AL
	INCQ R9
	CMPQ R9, DX
	JA   string_too_long
	
	CMPB AL, $'"'
	JE   found_quote_tail
	CMPB AL, $'\\'
	JE   found_escape_tail
	
	INCQ SI
	INCQ R8
	DECQ CX
	JNZ  tail_loop_str
	
need_more_data:
	MOVQ $-1, endOffset+24(FP)
	MOVB $0, escaped+32(FP)
	MOVQ $0, err+40(FP)
	RET

found_quote_tail:
	INCQ R8
	MOVQ R8, endOffset+24(FP)
	MOVB $0, escaped+32(FP)
	MOVQ $0, err+40(FP)
	RET

found_escape_tail:
	INCQ R8
	MOVQ R8, endOffset+24(FP)
	MOVB $1, escaped+32(FP)
	MOVQ $0, err+40(FP)
	RET

string_too_long:
	MOVQ $0, endOffset+24(FP)
	MOVB $0, escaped+32(FP)
	// Create error interface
	LEAQ runtime·errorString(SB), AX
	MOVQ AX, err+40(FP)
	LEAQ errStringTooLong(SB), BX
	MOVQ BX, err+48(FP)
	RET

DATA errStringTooLong+0(SB)/8, $"string t"
DATA errStringTooLong+8(SB)/8, $"oo long"
GLOBL errStringTooLong(SB), RODATA, $16
