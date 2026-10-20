// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

package lz4

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"
)

// testdata/cases.bin is written by testgen/gen.c (the phone's real encoder, phlz4.h in adaptation-a64 (usbcc/),
// compiled for the host): repeated {raw len u32, comp len u32, raw, comp}.
func TestCEncoderToGoDecoder(t *testing.T) {
	b, err := os.ReadFile("testdata/cases.bin")
	if err != nil {
		t.Skip("no testdata/cases.bin (run testgen/gen.c)")
	}
	n := 0
	for len(b) >= 8 {
		rl := int(binary.LittleEndian.Uint32(b))
		cl := int(binary.LittleEndian.Uint32(b[4:]))
		raw, comp := b[8:8+rl], b[8+rl:8+rl+cl]
		b = b[8+rl+cl:]
		got := make([]byte, rl)
		if err := Decode(got, comp); err != nil {
			t.Fatalf("case %d (raw %d, comp %d): %v", n, rl, cl, err)
		}
		if !bytes.Equal(got, raw) {
			t.Fatalf("case %d (raw %d): mismatch", n, rl)
		}
		n++
	}
	if n < 100 {
		t.Fatalf("only %d cases", n)
	}
	t.Logf("%d cases ok", n)
}

func TestCorrupt(t *testing.T) {
	dst := make([]byte, 8)
	for _, s := range [][]byte{nil, {0xF0}, {0x10, 'a', 5, 0}, {0x14, 'a', 0, 0}, {0x80}} {
		if err := Decode(dst, s); err == nil {
			t.Errorf("%x decoded", s)
		}
	}
}
