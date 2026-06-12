package tui

import "testing"

// TestFocusNextDoesNotPanicWhenInEnvSection reproduces the panic reported
// 2026-06-09 where pressing Tab while focused on the env section (focus ==
// fieldCount) caused "index out of range [N] with length N" because
// focusNext blindly Blurred em.inputs[em.focus].
func TestFocusNextDoesNotPanicWhenInEnvSection(t *testing.T) {
	em := newEditModel(nil)
	em.focus = int(fieldCount) // sentinel = env section focused
	em.envFocus = -1

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("focusNext panicked when focus was env-section sentinel: %v", r)
		}
	}()

	em.focusNext()
}

func TestFocusPrevDoesNotPanicWhenInEnvSection(t *testing.T) {
	em := newEditModel(nil)
	em.focus = int(fieldCount)
	em.envFocus = -1

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("focusPrev panicked when focus was env-section sentinel: %v", r)
		}
	}()

	em.focusPrev()
}
