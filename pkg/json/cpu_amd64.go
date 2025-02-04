//go:build amd64

package json

import "golang.org/x/sys/cpu"

func init() {
	if !cpu.X86.HasAVX2 {
		panic("CPU does not support AVX2")
	}
}
