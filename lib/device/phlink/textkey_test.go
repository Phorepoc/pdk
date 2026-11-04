package phlink

import "testing"

func TestTextKeyCoversPrintableASCII(t *testing.T) {
	for r := rune(0x20); r < 0x7f; r++ {
		if _, _, err := textKey(r); err != nil {
			t.Errorf("%q: %v", r, err)
		}
	}
	if c, s, _ := textKey(':'); c != 0x7d || !s {
		t.Errorf("':' = %#x shift=%v", c, s)
	}
	if c, s, _ := textKey('b'); c != 'B' || s {
		t.Errorf("'.' = %#x shift=%v", c, s)
	}
	if _, _, err := textKey('é'); err == nil {
		t.Error("é should fail")
	}
}
