package uiterm

import "testing"

// Regression: Close sent to a bounded channel and could block or enqueue
// duplicate shutdowns when called more than once.
// Regression: network callbacks modified terminal state directly. Post gives
// them a bounded handoff to the UI-owning Run goroutine instead of blocking.
func TestPostIsBoundedAndRejectsClosedUI(t *testing.T) {
	ui := New(nil)
	for i := 0; i < cap(ui.events); i++ {
		if !ui.Post(func() {}) {
			t.Fatal("queue filled too early")
		}
	}
	if ui.Post(func() {}) {
		t.Fatal("Post accepted work past queue capacity")
	}
	ui.Close()
	if ui.Post(func() {}) {
		t.Fatal("Post accepted work after close")
	}
}

func TestCloseIsNonblockingAndIdempotent(t *testing.T) {
	ui := New(nil)
	ui.Close()
	ui.Close()
	select {
	case <-ui.close:
	default:
		t.Fatal("Close did not signal shutdown")
	}
}

func TestSafeRuneRemovesTerminalControlCharacters(t *testing.T) {
	for _, r := range []rune{'\x1b', '\x7f', '\u202e'} {
		if got := safeRune(r); got != ' ' {
			t.Errorf("safeRune(%U) = %U, want space", r, got)
		}
	}
	if got := safeRune('A'); got != 'A' {
		t.Fatalf("safeRune altered printable text: %U", got)
	}
}
