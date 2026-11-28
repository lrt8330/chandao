package domain

import "strings"

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

// Finished 判断状态是否已终结。终结中的事项会在增量同步时从指派列表
// 移除（与全量抓取「只保留未完成」的语义一致）。
func Finished(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "done", "cancel", "canceled", "cancelled", "closed", "resolved":
		return true
	}
	return false
}
