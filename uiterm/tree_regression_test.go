package uiterm

import (
	"testing"
	"time"
)

// cyclicItem reports itself as its own child, standing in for a channel graph
// in which a channel is its own ancestor.
type cyclicItem struct{ name string }

func (i *cyclicItem) String() string { return i.name }

func (i *cyclicItem) TreeItemStyle(fg, bg Attribute, active bool) (Attribute, Attribute) {
	return fg, bg
}

// Regression: rebuild_rec followed parent/child links with no depth limit, so
// a cyclic channel graph recursed until the process ran out of memory. A
// rebuild must now terminate and stay bounded.
func TestTreeRebuildTerminatesOnCyclicGraph(t *testing.T) {
	t.Parallel()

	self := &cyclicItem{name: "loop"}
	tree := Tree{
		Generator: func(item TreeItem) []TreeItem {
			return []TreeItem{self}
		},
	}

	done := make(chan struct{})
	go func() {
		tree.rebuild(false, nil)
		close(done)
	}()

	select {
	case <-done:
	case <-timeoutAfterSeconds(10):
		t.Fatal("rebuild did not terminate on a cyclic tree")
	}

	if len(tree.lines) == 0 {
		t.Fatal("expected the bounded rebuild to still produce lines")
	}
	if len(tree.lines) > maxTreeLines {
		t.Fatalf("rebuild produced %d lines, above the %d cap",
			len(tree.lines), maxTreeLines)
	}
	for _, line := range tree.lines {
		if line.Level >= maxTreeDepth {
			t.Fatalf("rebuild recursed to level %d, at or past the %d cap",
				line.Level, maxTreeDepth)
		}
	}
}

func timeoutAfterSeconds(n int) <-chan struct{} {
	ch := make(chan struct{})
	go func() {
		time.Sleep(time.Duration(n) * time.Second)
		close(ch)
	}()
	return ch
}
