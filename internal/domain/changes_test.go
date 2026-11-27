package domain

import (
	"reflect"
	"testing"
)

func TestItemKeySeparatesItemTypes(t *testing.T) {
	task := Item{
		ID:        "1",
		Type:      ItemTask,
		Title:     "Fix login",
		Project:   "Web",
		Status:    "doing",
		UpdatedAt: "2026-11-27T10:00:00Z",
		URL:       "https://example.com/task-1.html",
	}
	bug := Item{
		ID:        "1",
		Type:      ItemBug,
		Title:     "Login fails",
		Project:   "Web",
		Status:    "active",
		UpdatedAt: "2026-11-27T10:00:00Z",
		URL:       "https://example.com/bug-1.html",
	}

	if got, want := task.Key(), "task:1"; got != want {
		t.Fatalf("task.Key() = %q, want %q", got, want)
	}
	if got, want := bug.Key(), "bug:1"; got != want {
		t.Fatalf("bug.Key() = %q, want %q", got, want)
	}
}

func TestDomainValueConstants(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "task type", got: string(ItemTask), want: "task"},
		{name: "bug type", got: string(ItemBug), want: "bug"},
		{name: "new change", got: string(ChangeNew), want: "new"},
		{name: "status change", got: string(ChangeStatus), want: "status"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("constant = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestDetectChangesFirstBaseline(t *testing.T) {
	item := testItem("1", ItemTask, "doing")
	current := map[string]Item{item.Key(): item}

	if got := DetectChanges(nil, current, true); len(got) != 0 {
		t.Fatalf("DetectChanges() on first baseline = %#v, want no changes", got)
	}
}

func TestDetectChangesNewItem(t *testing.T) {
	item := testItem("1", ItemTask, "doing")
	current := map[string]Item{item.Key(): item}
	want := []Change{{Kind: ChangeNew, Item: item}}

	if got := DetectChanges(nil, current, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectChanges() = %#v, want %#v", got, want)
	}
}

func TestDetectChangesStatusChange(t *testing.T) {
	previousItem := testItem("1", ItemTask, "doing")
	currentItem := previousItem
	currentItem.Status = "done"
	currentItem.Title = "Updated title"
	previous := map[string]Item{previousItem.Key(): previousItem}
	current := map[string]Item{currentItem.Key(): currentItem}
	want := []Change{{
		Kind: ChangeStatus,
		Item: currentItem,
		Old:  "doing",
		New:  "done",
	}}

	if got := DetectChanges(previous, current, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectChanges() = %#v, want %#v", got, want)
	}
}

func TestDetectChangesDuplicateAndNoOp(t *testing.T) {
	previousItem := testItem("1", ItemTask, "doing")
	tests := []struct {
		name        string
		currentItem Item
	}{
		{name: "duplicate item", currentItem: previousItem},
		{name: "metadata only", currentItem: Item{
			ID:        previousItem.ID,
			Type:      previousItem.Type,
			Title:     "Updated title",
			Project:   "Updated project",
			Status:    previousItem.Status,
			UpdatedAt: "2026-11-27T11:00:00Z",
			URL:       "https://example.com/updated-task-1.html",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := map[string]Item{previousItem.Key(): previousItem}
			current := map[string]Item{tt.currentItem.Key(): tt.currentItem}

			if got := DetectChanges(previous, current, false); len(got) != 0 {
				t.Fatalf("DetectChanges() = %#v, want no changes", got)
			}
		})
	}
}

func TestDetectChangesDisappearedItem(t *testing.T) {
	remaining := testItem("1", ItemTask, "doing")
	disappeared := testItem("2", ItemBug, "active")
	previous := map[string]Item{
		remaining.Key():   remaining,
		disappeared.Key(): disappeared,
	}
	current := map[string]Item{remaining.Key(): remaining}

	if got := DetectChanges(previous, current, false); len(got) != 0 {
		t.Fatalf("DetectChanges() = %#v, want no changes for disappeared item", got)
	}
}

func TestDetectChangesEmptyList(t *testing.T) {
	item := testItem("1", ItemTask, "doing")
	tests := []struct {
		name     string
		previous map[string]Item
		current  map[string]Item
	}{
		{name: "nil maps"},
		{name: "empty maps", previous: map[string]Item{}, current: map[string]Item{}},
		{name: "all items disappeared", previous: map[string]Item{item.Key(): item}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DetectChanges(tt.previous, tt.current, false); len(got) != 0 {
				t.Fatalf("DetectChanges() = %#v, want no changes", got)
			}
		})
	}
}

func TestDetectChangesSortedByItemKey(t *testing.T) {
	statusItem := testItem("2", ItemTask, "done")
	previousStatusItem := statusItem
	previousStatusItem.Status = "doing"
	previous := map[string]Item{previousStatusItem.Key(): previousStatusItem}
	bug1 := testItem("1", ItemBug, "active")
	bug3 := testItem("3", ItemBug, "resolved")
	task1 := testItem("1", ItemTask, "doing")
	current := map[string]Item{
		statusItem.Key(): statusItem,
		task1.Key():      task1,
		bug3.Key():       bug3,
		bug1.Key():       bug1,
	}
	want := []Change{
		{Kind: ChangeNew, Item: bug1},
		{Kind: ChangeNew, Item: bug3},
		{Kind: ChangeNew, Item: task1},
		{Kind: ChangeStatus, Item: statusItem, Old: "doing", New: "done"},
	}

	if got := DetectChanges(previous, current, false); !reflect.DeepEqual(got, want) {
		t.Fatalf("DetectChanges() = %#v, want changes sorted by key %#v", got, want)
	}
}

func testItem(id string, itemType ItemType, status string) Item {
	return Item{
		ID:        id,
		Type:      itemType,
		Title:     "Item " + id,
		Project:   "Web",
		Status:    status,
		UpdatedAt: "2026-11-27T10:00:00Z",
		URL:       "https://example.com/item-" + id + ".html",
	}
}
