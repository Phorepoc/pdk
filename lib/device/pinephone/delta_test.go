package pinephone

import "testing"

func TestBlockHashFNV(t *testing.T) {
	// FNV-1a over words: empty = offset basis; one zero word = basis * prime
	if h := BlockHash(nil); h != 2166136261 {
		t.Fatalf("empty: %08x", h)
	}
	basis, prime := uint32(2166136261), uint32(16777619)
	if h := BlockHash(make([]byte, 4)); h != basis*prime {
		t.Fatalf("one zero word: %08x", h)
	}
}

func TestPad512AndBlocks(t *testing.T) {
	img := make([]byte, deltaBlock*2+100)
	p := pad512(img)
	if len(p) != deltaBlock*2+512 {
		t.Fatalf("padded to %d", len(p))
	}
	if n := len(BlockHashes(p)); n != 3 {
		t.Fatalf("%d blocks", n)
	}
}

func TestDeltaRuns(t *testing.T) {
	have := []uint32{1, 2, 3, 4, 5}
	want := []uint32{1, 9, 9, 4, 5, 6, 7}
	r := deltaRuns(have, want)
	if len(r) != 2 || r[0] != [2]int{1, 3} || r[1] != [2]int{5, 7} {
		t.Fatalf("runs %v", r)
	}
	if r := deltaRuns(want, want); len(r) != 0 {
		t.Fatalf("identical: %v", r)
	}
	// a long change is split at deltaRun
	n := deltaRun/deltaBlock*2 + 1
	r = deltaRuns(nil, make([]uint32, n))
	if len(r) != 3 || r[2][1] != n {
		t.Fatalf("split: %v", r)
	}
}
