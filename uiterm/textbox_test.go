package uiterm

import "testing"

func TestTextboxHistoryNavigatesSubmittedText(t *testing.T) {
	t.Parallel()

	textbox := Textbox{}
	textbox.addHistory("first")
	textbox.addHistory("second")

	if !textbox.previousHistory() {
		t.Fatal("expected previous history item")
	}
	if textbox.Text != "second" {
		t.Fatalf("expected second history item, got %q", textbox.Text)
	}

	if !textbox.previousHistory() {
		t.Fatal("expected older history item")
	}
	if textbox.Text != "first" {
		t.Fatalf("expected first history item, got %q", textbox.Text)
	}

	if !textbox.nextHistory() {
		t.Fatal("expected newer history item")
	}
	if textbox.Text != "second" {
		t.Fatalf("expected second history item after next, got %q", textbox.Text)
	}
}

func TestTextboxHistoryNavigatesAltArrowKeys(t *testing.T) {
	t.Parallel()

	textbox := Textbox{}
	textbox.addHistory("first")
	textbox.addHistory("second")

	if redraw := textbox.handleHistoryKey(KeyAltArrowUp); !redraw {
		t.Fatal("expected alt-up to recall history")
	}
	if textbox.Text != "second" {
		t.Fatalf("expected second history item, got %q", textbox.Text)
	}

	if redraw := textbox.handleHistoryKey(KeyAltArrowDown); !redraw {
		t.Fatal("expected alt-down to move forward through history")
	}
	if textbox.Text != "" {
		t.Fatalf("expected alt-down to restore empty draft, got %q", textbox.Text)
	}
}

func TestTextboxHistoryRestoresDraft(t *testing.T) {
	t.Parallel()

	textbox := Textbox{Text: "draft"}
	textbox.addHistory("previous")
	textbox.setText("draft")

	if !textbox.previousHistory() {
		t.Fatal("expected previous history item")
	}
	if textbox.Text != "previous" {
		t.Fatalf("expected previous history item, got %q", textbox.Text)
	}

	if !textbox.nextHistory() {
		t.Fatal("expected history next to restore draft")
	}
	if textbox.Text != "draft" {
		t.Fatalf("expected draft to be restored, got %q", textbox.Text)
	}
}

func TestTextboxHistorySkipsBlankAndConsecutiveDuplicateEntries(t *testing.T) {
	t.Parallel()

	textbox := Textbox{}
	textbox.addHistory("")
	textbox.addHistory("   ")
	textbox.addHistory("repeat")
	textbox.addHistory("repeat")

	if len(textbox.history) != 1 {
		t.Fatalf("expected one history item, got %d", len(textbox.history))
	}
	if textbox.history[0] != "repeat" {
		t.Fatalf("expected repeat history item, got %q", textbox.history[0])
	}
}
