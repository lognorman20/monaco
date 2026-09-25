package httpapi

import "testing"

func TestFoldUUIDHomoglyphs_cyrillicA(t *testing.T) {
	t.Parallel()
	got := foldUUIDHomoglyphs("43902952-e934-499c-а520-991bcb0cfde0")
	want := "43902952-e934-499c-a520-991bcb0cfde0"
	if got != want {
		t.Fatalf("foldUUIDHomoglyphs() = %q, want %q", got, want)
	}
}
