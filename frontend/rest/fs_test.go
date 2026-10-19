package rest

import "testing"

func TestCleanPath(t *testing.T) {
	for in, want := range map[string]string{"": "/", "a/b": "/a/b", "/x/../y": "/y", `C:\d`: "/C:/d"} {
		if got := cleanPath(in); got != want {
			t.Errorf("%q: %q != %q", in, got, want)
		}
	}
}
