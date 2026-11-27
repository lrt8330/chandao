package domain

import "sort"

type ChangeKind string

const (
	ChangeNew    ChangeKind = "new"
	ChangeStatus ChangeKind = "status"
)

type Change struct {
	Kind ChangeKind
	Item Item
	Old  string
	New  string
}

func DetectChanges(previous, current map[string]Item, first bool) []Change {
	if first {
		return nil
	}

	var changes []Change
	for key, item := range current {
		previousItem, exists := previous[key]
		switch {
		case !exists:
			changes = append(changes, Change{Kind: ChangeNew, Item: item})
		case previousItem.Status != item.Status:
			changes = append(changes, Change{
				Kind: ChangeStatus,
				Item: item,
				Old:  previousItem.Status,
				New:  item.Status,
			})
		}
	}

	sort.Slice(changes, func(i, j int) bool {
		return changes[i].Item.Key() < changes[j].Item.Key()
	})
	return changes
}
