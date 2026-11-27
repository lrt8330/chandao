package domain

type ItemType string

const (
	ItemTask ItemType = "task"
	ItemBug  ItemType = "bug"
)

type Item struct {
	ID        string
	Type      ItemType
	Title     string
	Project   string
	Status    string
	UpdatedAt string
	URL       string
}

func (i Item) Key() string { return string(i.Type) + ":" + i.ID }
