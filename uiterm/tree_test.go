package uiterm

import "testing"

type testTreeItem string

func (i testTreeItem) String() string {
	return string(i)
}

func (i testTreeItem) TreeItemStyle(fg, bg Attribute, active bool) (Attribute, Attribute) {
	return fg, bg
}

func TestTreeRebuildResetsActiveLine(t *testing.T) {
	t.Parallel()

	tree := Tree{
		Generator: func(item TreeItem) []TreeItem {
			if item != nil {
				return nil
			}
			return []TreeItem{testTreeItem("one"), testTreeItem("two"), testTreeItem("three")}
		},
	}

	tree.Rebuild()
	tree.SetActiveLine(2, false)
	tree.Rebuild()

	if tree.activeLine != 0 {
		t.Fatalf("expected active line to reset to 0, got %d", tree.activeLine)
	}
}

func TestTreeRebuildPreservingActiveItemKeepsMatchingItem(t *testing.T) {
	t.Parallel()

	items := []TreeItem{testTreeItem("one"), testTreeItem("two"), testTreeItem("three")}
	tree := Tree{
		Generator: func(item TreeItem) []TreeItem {
			if item != nil {
				return nil
			}
			return items
		},
	}

	tree.Rebuild()
	tree.SetActiveLine(1, false)
	items = []TreeItem{testTreeItem("three"), testTreeItem("one"), testTreeItem("two")}
	tree.RebuildPreservingActiveItem(func(previous, current TreeItem) bool {
		return previous.String() == current.String()
	})

	if tree.activeLine != 2 {
		t.Fatalf("expected active line to follow matching item to line 2, got %d", tree.activeLine)
	}
	if got := tree.ActiveItem().String(); got != "two" {
		t.Fatalf("expected active item two, got %q", got)
	}
}

func TestTreeSetActiveItemSelectsMatchingItem(t *testing.T) {
	t.Parallel()

	tree := Tree{
		Generator: func(item TreeItem) []TreeItem {
			if item != nil {
				return nil
			}
			return []TreeItem{testTreeItem("one"), testTreeItem("two"), testTreeItem("three")}
		},
	}

	tree.Rebuild()
	if ok := tree.SetActiveItem(testTreeItem("three"), func(previous, current TreeItem) bool {
		return previous.String() == current.String()
	}); !ok {
		t.Fatal("expected matching item to be selected")
	}

	if tree.activeLine != 2 {
		t.Fatalf("expected active line 2, got %d", tree.activeLine)
	}
}
