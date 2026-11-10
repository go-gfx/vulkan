package vk

import "testing"

type fieldOffset struct {
	name string
	got  uintptr
	want uintptr
}

// checkLayout compares the Go layout with the table the generator computed
// from the registry by the C rules (layout_gen_test.go).
func checkLayout(t *testing.T, name string, size uintptr, wantSize int, align uintptr, wantAlign int, fields []fieldOffset) {
	t.Helper()
	if size != uintptr(wantSize) {
		t.Errorf("%s: sizeof = %d, the C rules give %d", name, size, wantSize)
	}
	if align != uintptr(wantAlign) {
		t.Errorf("%s: alignof = %d, the C rules give %d", name, align, wantAlign)
	}
	for _, f := range fields {
		if f.got != f.want {
			t.Errorf("%s.%s: offsetof = %d, the C rules give %d", name, f.name, f.got, f.want)
		}
	}
}
