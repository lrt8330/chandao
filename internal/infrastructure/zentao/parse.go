package zentao

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"chandao/zentaotray/internal/domain"
	"golang.org/x/net/html"
	"golang.org/x/net/html/charset"
)

var (
	ErrUnexpectedStructure = errors.New("unexpected zentao assigned page structure")
	ErrInvalidDetailURL    = errors.New("invalid zentao detail URL")
	ErrDuplicateItemID     = errors.New("duplicate zentao item ID")
	ErrPaginationRequired  = errors.New("zentao assigned page requires pagination")
)

// ParseAssignedTasks extracts unfinished tasks from an assigned-to page.
func ParseAssignedTasks(body []byte, pageURL string) ([]domain.Item, error) {
	return parseAssigned(body, pageURL, assignedSpec{
		tableID:      "tasktable",
		objectType:   "task",
		itemType:     domain.ItemTask,
		titleClasses: []string{"c-name", "c-title"},
	})
}

// ParseAssignedBugs extracts active bugs from an assigned-to page.
func ParseAssignedBugs(body []byte, pageURL string) ([]domain.Item, error) {
	return parseAssigned(body, pageURL, assignedSpec{
		tableID:      "bugList",
		objectType:   "bug",
		itemType:     domain.ItemBug,
		titleClasses: []string{"c-title", "c-name"},
	})
}

type assignedSpec struct {
	tableID      string
	objectType   string
	itemType     domain.ItemType
	titleClasses []string
}

// HasMorePages reports whether recTotal exceeds recPerPage. It does not fetch
// additional pages; callers must handle ErrPaginationRequired from the parsers.
func HasMorePages(body []byte) (bool, error) {
	decoded, err := decodeAssignedHTML(body)
	if err != nil {
		return false, fmt.Errorf("%w: %v", ErrUnexpectedStructure, err)
	}
	doc, err := html.Parse(bytes.NewReader(decoded))
	if err != nil {
		return false, fmt.Errorf("%w: parse HTML: %v", ErrUnexpectedStructure, err)
	}
	metadata, err := readPaginationMetadata(doc)
	if err != nil {
		return false, fmt.Errorf("%w: pagination metadata: %v", ErrUnexpectedStructure, err)
	}
	return metadata.total > metadata.perPage, nil
}

type paginationMetadata struct {
	total      int
	perPage    int
	hasTotal   bool
	hasPerPage bool
}

func readPaginationMetadata(doc *html.Node) (paginationMetadata, error) {
	values := make(map[string]string)
	record := func(marker, value string) {
		value = strings.TrimSpace(value)
		if previous, exists := values[marker]; exists && previous != value {
			values[marker] = previous + "\x00" + value
			return
		}
		values[marker] = value
	}
	walkElements(doc, func(node *html.Node) {
		for _, attribute := range node.Attr {
			key := strings.ToLower(strings.ReplaceAll(attribute.Key, "_", "-"))
			switch key {
			case "data-rec-total":
				record("total", attribute.Val)
			case "data-rec-per-page":
				record("perPage", attribute.Val)
			case "id", "name", "class":
				for _, token := range strings.Fields(strings.ToLower(attribute.Val)) {
					marker := ""
					switch token {
					case "rectotal", "rec-total":
						marker = "total"
					case "recperpage", "rec-per-page":
						marker = "perPage"
					}
					if marker == "" {
						continue
					}
					value := strings.TrimSpace(attr(node, "value"))
					if value == "" {
						value = normalizedText(node)
					}
					record(marker, value)
				}
			}
		}
	})

	metadata := paginationMetadata{}
	for marker, raw := range values {
		if strings.ContainsRune(raw, 0) {
			return metadata, fmt.Errorf("conflicting %s values", marker)
		}
		value, err := strconv.Atoi(strings.ReplaceAll(raw, ",", ""))
		if err != nil || value < 0 {
			return metadata, fmt.Errorf("%s must be a non-negative integer", marker)
		}
		switch marker {
		case "total", "data-rec-total":
			metadata.total = value
			metadata.hasTotal = true
		case "perPage", "data-rec-per-page":
			metadata.perPage = value
			metadata.hasPerPage = true
		}
	}
	if metadata.hasTotal != metadata.hasPerPage {
		return metadata, errors.New("recTotal and recPerPage must both be present")
	}
	if metadata.hasTotal && metadata.total > 0 && metadata.perPage == 0 {
		return metadata, errors.New("recPerPage must be positive when recTotal is positive")
	}
	return metadata, nil
}

func decodeAssignedHTML(body []byte) ([]byte, error) {
	if len(body) == 0 {
		return body, nil
	}
	if utf8.Valid(body) {
		return bytes.TrimPrefix(body, []byte{0xef, 0xbb, 0xbf}), nil
	}
	_, name, _ := charset.DetermineEncoding(body, "text/html")
	name = strings.ToLower(strings.TrimSpace(name))
	if name != "gb18030" && name != "gbk" {
		return nil, errors.New("unsupported or undeclared non-UTF-8 encoding")
	}
	reader, err := charset.NewReaderLabel(name, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	decoded, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", name, err)
	}
	if !utf8.Valid(decoded) || bytes.ContainsRune(decoded, utf8.RuneError) || bytes.IndexByte(decoded, 0) >= 0 {
		return nil, fmt.Errorf("invalid %s byte sequence", name)
	}
	return decoded, nil
}

func parseAssigned(body []byte, pageURL string, spec assignedSpec) ([]domain.Item, error) {
	if _, err := parsePageURL(pageURL); err != nil {
		return nil, err
	}
	decoded, err := decodeAssignedHTML(body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnexpectedStructure, err)
	}
	if isLoginPage(decoded) {
		return nil, ErrSessionExpired
	}
	doc, err := html.Parse(bytes.NewReader(decoded))
	if err != nil {
		return nil, fmt.Errorf("%w: parse HTML: %v", ErrUnexpectedStructure, err)
	}
	table := findByID(doc, spec.tableID)
	if table != nil && table.Data != "table" {
		table = firstElement(table, "table")
		if table == nil {
			return nil, fmt.Errorf("%w: %s table not found", ErrUnexpectedStructure, spec.objectType)
		}
	}
	rows := modernRows(table)
	var columns *legacyColumns
	if table == nil || len(rows) == 0 {
		legacyRoot := doc
		if table != nil {
			legacyRoot = table
		}
		legacyTable, mapped, found := findLegacyTable(legacyRoot)
		if found {
			table = legacyTable
			columns = &mapped
			rows = legacyRows(table)
			if len(rows) == 0 && columns.status < 0 {
				return nil, fmt.Errorf("legacy %s table has no status column", spec.objectType)
			}
		} else if table != nil && len(allRows(table)) == 0 {
			rows = nil
		} else {
			return nil, fmt.Errorf("%w: %s table not found or not recognized", ErrUnexpectedStructure, spec.objectType)
		}
	}
	pagination, err := readPaginationMetadata(doc)
	if err != nil {
		return nil, fmt.Errorf("%w: pagination metadata: %v", ErrUnexpectedStructure, err)
	}
	items := make([]domain.Item, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for index, row := range rows {
		var item domain.Item
		var err error
		if columns != nil {
			item, err = parseLegacyItem(row, pageURL, spec, *columns)
		} else {
			item, err = parseModernItem(row, pageURL, spec)
		}
		if err != nil {
			return nil, fmt.Errorf("%s row %d: %w", spec.objectType, index+1, err)
		}
		if _, exists := seen[item.ID]; exists {
			return nil, fmt.Errorf("%s row %d: %w: %s", spec.objectType, index+1, ErrDuplicateItemID, item.ID)
		}
		seen[item.ID] = struct{}{}
		unfinished, err := spec.unfinished(item.Status)
		if err != nil {
			return nil, fmt.Errorf("%s row %d: %w", spec.objectType, index+1, err)
		}
		if unfinished {
			items = append(items, item)
		}
	}
	if pagination.hasTotal && pagination.total > len(rows) {
		return items, fmt.Errorf("%w: %d total rows but only %d are present", ErrPaginationRequired, pagination.total, len(rows))
	}
	return items, nil
}

func parseModernItem(row *html.Node, pageURL string, spec assignedSpec) (domain.Item, error) {
	link, id, detailURL, err := detailLink(row, spec.objectType, pageURL)
	if err != nil {
		return domain.Item{}, err
	}
	title := normalizedText(link)
	if title == "" {
		title = normalizedCellText(row, spec.titleClasses...)
	}
	statusCell := cellByClass(row, "c-status")
	status := statusValue(row, statusCell)
	if status == "" {
		return domain.Item{}, errors.New("status is missing or is not a raw status code")
	}
	item := domain.Item{
		ID:        id,
		Type:      spec.itemType,
		Title:     title,
		Project:   normalizedCellText(row, "c-project"),
		Status:    status,
		UpdatedAt: normalizedCellText(row, "c-lastEditedDate"),
		URL:       detailURL,
	}
	if err := validateItem(item); err != nil {
		return domain.Item{}, err
	}
	return item, nil
}

func validateItem(item domain.Item) error {
	switch {
	case item.Title == "":
		return errors.New("title is missing")
	case item.Project == "":
		return errors.New("project is missing")
	case item.Status == "":
		return errors.New("status is missing")
	case item.UpdatedAt == "":
		return errors.New("updated date is missing")
	case item.URL == "":
		return errors.New("detail URL is missing")
	default:
		return nil
	}
}

func (spec assignedSpec) unfinished(status string) (bool, error) {
	switch strings.ToLower(status) {
	case "wait", "doing", "pause":
		return spec.itemType == domain.ItemTask, nil
	case "active":
		return spec.itemType == domain.ItemBug, nil
	case "done", "cancel", "canceled", "cancelled", "closed", "resolved":
		return false, nil
	default:
		return false, errors.New("unknown raw status code")
	}
}

func findByID(root *html.Node, id string) *html.Node {
	if root.Type == html.ElementNode && attr(root, "id") == id {
		return root
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if found := findByID(child, id); found != nil {
			return found
		}
	}
	return nil
}

func firstElement(root *html.Node, tag string) *html.Node {
	var found *html.Node
	walkElements(root, func(node *html.Node) {
		if found == nil && node.Data == tag {
			found = node
		}
	})
	return found
}

func modernRows(table *html.Node) []*html.Node {
	return collectRows(table, true)
}

func allRows(table *html.Node) []*html.Node {
	return collectRows(table, false)
}

func collectRows(root *html.Node, dataIDOnly bool) []*html.Node {
	if root == nil {
		return nil
	}
	var rows []*html.Node
	var walk func(*html.Node, bool)
	walk = func(node *html.Node, insideTable bool) {
		if node.Type == html.ElementNode && node.Data == "table" {
			if node != root && insideTable {
				return
			}
			insideTable = true
		}
		if node.Type == html.ElementNode && node.Data == "tr" {
			_, hasDataID := getAttr(node, "data-id")
			if !dataIDOnly || hasDataID {
				rows = append(rows, node)
			}
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			walk(child, insideTable)
		}
	}
	walk(root, root.Type == html.ElementNode && root.Data == "table")
	return rows
}

type legacyColumns struct {
	id      int
	title   int
	status  int
	project int
	updated int
}

func findLegacyTable(doc *html.Node) (*html.Node, legacyColumns, bool) {
	var foundTable *html.Node
	var foundColumns legacyColumns
	walkElements(doc, func(node *html.Node) {
		if foundTable != nil || node.Data != "table" {
			return
		}
		columns, recognized := mapLegacyColumns(node)
		if recognized {
			foundTable = node
			foundColumns = columns
		}
	})
	return foundTable, foundColumns, foundTable != nil
}

func mapLegacyColumns(table *html.Node) (legacyColumns, bool) {
	columns := legacyColumns{id: -1, title: -1, status: -1, project: -1, updated: -1}
	headerRows := legacyHeaderRows(table)
	for _, row := range headerRows {
		index := 0
		for cell := directChild(row, "th"); cell != nil; cell = nextSiblingElement(cell, "th") {
			label := strings.ToLower(strings.TrimSpace(normalizedText(cell) + " " + attr(cell, "class")))
			switch {
			case hasClass(cell, "c-id") || label == "id" || strings.Contains(label, "task id") || strings.Contains(label, "bug id") || strings.Contains(label, "任务id") || strings.Contains(label, "bugid") || strings.Contains(label, "编号"):
				columns.id = index
			case hasClass(cell, "c-name") || hasClass(cell, "c-title") || strings.Contains(label, "name") || strings.Contains(label, "title") || strings.Contains(label, "名称") || strings.Contains(label, "标题"):
				columns.title = index
			case hasClass(cell, "c-status") || strings.Contains(label, "status") || strings.Contains(label, "状态"):
				columns.status = index
			case hasClass(cell, "c-project") || strings.Contains(label, "project") || strings.Contains(label, "项目"):
				columns.project = index
			case hasClass(cell, "c-lastEditedDate") || strings.Contains(label, "lastediteddate") || strings.Contains(label, "last edited") || strings.Contains(label, "最后编辑") || strings.Contains(label, "最后修改") || strings.Contains(label, "更新时间"):
				columns.updated = index
			}
			index++
		}
	}
	return columns, columns.id >= 0 && columns.title >= 0
}

func legacyHeaderRows(table *html.Node) []*html.Node {
	var thead *html.Node
	walkElements(table, func(node *html.Node) {
		if thead == nil && node.Data == "thead" {
			thead = node
		}
	})
	if thead != nil {
		var rows []*html.Node
		for row := directChild(thead, "tr"); row != nil; row = nextSiblingElement(row, "tr") {
			rows = append(rows, row)
		}
		return rows
	}
	for _, row := range allRows(table) {
		if directChild(row, "th") != nil {
			return []*html.Node{row}
		}
	}
	return nil
}

func legacyRows(table *html.Node) []*html.Node {
	var rows []*html.Node
	for _, row := range allRows(table) {
		if row.Parent == nil || row.Parent.Data != "tbody" || directChild(row, "th") != nil {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

func parseLegacyItem(row *html.Node, pageURL string, spec assignedSpec, columns legacyColumns) (domain.Item, error) {
	if columns.status < 0 {
		return domain.Item{}, fmt.Errorf("legacy %s table has no status column", spec.objectType)
	}
	link, id, detailURL, err := detailLink(row, spec.objectType, pageURL)
	if err != nil {
		return domain.Item{}, err
	}
	titleCell := cellAt(row, columns.title)
	title := normalizedText(link)
	if title == "" && titleCell != nil {
		title = normalizedText(titleCell)
	}
	statusCell := cellAt(row, columns.status)
	if statusCell == nil {
		return domain.Item{}, errors.New("status cell is missing")
	}
	status := statusValue(row, statusCell)
	if status == "" {
		return domain.Item{}, errors.New("status is missing or is not a raw status code")
	}
	item := domain.Item{
		ID:        id,
		Type:      spec.itemType,
		Title:     title,
		Project:   normalizedText(cellAt(row, columns.project)),
		Status:    status,
		UpdatedAt: normalizedText(cellAt(row, columns.updated)),
		URL:       detailURL,
	}
	if err := validateItem(item); err != nil {
		return domain.Item{}, err
	}
	return item, nil
}

func cellAt(row *html.Node, index int) *html.Node {
	if index < 0 {
		return nil
	}
	cell := directChild(row, "td")
	for current := 0; cell != nil && current < index; current++ {
		cell = nextSiblingElement(cell, "td")
	}
	return cell
}

func cellByClass(row *html.Node, classes ...string) *html.Node {
	for cell := directChild(row, "td"); cell != nil; cell = nextSiblingElement(cell, "td") {
		for _, class := range classes {
			if hasClass(cell, class) {
				return cell
			}
		}
	}
	return nil
}

func normalizedCellText(row *html.Node, classes ...string) string {
	for cell := directChild(row, "td"); cell != nil; cell = nextSiblingElement(cell, "td") {
		for _, class := range classes {
			if hasClass(cell, class) {
				if text := normalizedText(cell); text != "" {
					return text
				}
			}
		}
	}
	return ""
}

func normalizedText(node *html.Node) string {
	if node == nil {
		return ""
	}
	var text strings.Builder
	var walk func(*html.Node)
	walk = func(current *html.Node) {
		if current.Type == html.TextNode {
			text.WriteString(current.Data)
			return
		}
		if current.Type == html.ElementNode && (current.Data == "br" || current.Data == "p" || current.Data == "div") {
			text.WriteByte(' ')
		}
		for child := current.FirstChild; child != nil; child = child.NextSibling {
			walk(child)
		}
	}
	walk(node)
	return strings.Join(strings.Fields(text.String()), " ")
}

func statusValue(row, cell *html.Node) string {
	if row == nil {
		return ""
	}
	if value := strings.TrimSpace(attr(row, "data-status")); value != "" {
		return value
	}
	if status := statusClass(row); status != "" {
		return status
	}
	if cell == nil {
		return ""
	}
	var status string
	walkElements(cell, func(node *html.Node) {
		if status != "" {
			return
		}
		if value := strings.TrimSpace(attr(node, "data-status")); value != "" {
			status = value
			return
		}
		status = statusClass(node)
	})
	if status != "" {
		return status
	}
	return normalizedText(cell)
}

func statusClass(node *html.Node) string {
	for _, class := range strings.Fields(attr(node, "class")) {
		if strings.HasPrefix(strings.ToLower(class), "status-") && len(class) > len("status-") {
			return class[len("status-"):]
		}
	}
	return ""
}

func detailLink(row *html.Node, objectType, pageURL string) (*html.Node, string, string, error) {
	var link *html.Node
	var fallback *html.Node
	var fallbackErr error
	var id, resolved string
	walkElements(row, func(node *html.Node) {
		if link != nil || node.Data != "a" {
			return
		}
		href := attr(node, "href")
		if !strings.Contains(strings.ToLower(href), objectType+"-view") {
			return
		}
		candidateID, candidateURL, err := validateDetailURL(href, objectType, pageURL)
		if err != nil {
			if fallback == nil {
				fallback = node
				fallbackErr = err
			}
			return
		}
		link = node
		id = candidateID
		resolved = candidateURL
	})
	if link != nil {
		return link, id, resolved, nil
	}
	if fallback != nil {
		return nil, "", "", fallbackErr
	}
	return nil, "", "", fmt.Errorf("%w: %s detail link not found", ErrInvalidDetailURL, objectType)
}

func parsePageURL(pageURL string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimSpace(pageURL))
	if err != nil || base.Scheme == "" || base.Host == "" || base.User != nil ||
		(base.Scheme != "http" && base.Scheme != "https") || !hasBizPrefix(base.Path) {
		return nil, fmt.Errorf("%w: invalid page URL", ErrInvalidDetailURL)
	}
	return base, nil
}

func validateDetailURL(href, objectType, pageURL string) (string, string, error) {
	base, err := parsePageURL(pageURL)
	if err != nil {
		return "", "", err
	}
	reference, err := url.Parse(strings.TrimSpace(href))
	if err != nil || reference.IsAbs() && reference.Scheme == "" {
		return "", "", fmt.Errorf("%w: malformed link", ErrInvalidDetailURL)
	}
	resolved := base.ResolveReference(reference)
	if resolved.User != nil || (resolved.Scheme != "http" && resolved.Scheme != "https") ||
		!sameOrigin(resolved, base) || !hasBizPrefix(resolved.Path) || resolved.Fragment != "" {
		return "", "", fmt.Errorf("%w: link is not a same-origin %s URL under /biz", ErrInvalidDetailURL, objectType)
	}

	filename := path.Base(resolved.Path)
	prefix := objectType + "-view-"
	var rawID string
	switch {
	case strings.HasPrefix(filename, prefix) && strings.HasSuffix(filename, ".html"):
		rawID = strings.TrimSuffix(strings.TrimPrefix(filename, prefix), ".html")
	case filename == objectType+"-view.html":
		query, parseErr := url.ParseQuery(resolved.RawQuery)
		if parseErr != nil {
			return "", "", fmt.Errorf("%w: malformed query", ErrInvalidDetailURL)
		}
		values := query[objectType+"ID"]
		if len(values) != 1 {
			return "", "", fmt.Errorf("%w: missing or repeated %sID", ErrInvalidDetailURL, objectType)
		}
		rawID = values[0]
	default:
		return "", "", fmt.Errorf("%w: wrong object type or detail path", ErrInvalidDetailURL)
	}
	id, err := strconv.ParseUint(rawID, 10, 64)
	if err != nil || id == 0 {
		return "", "", fmt.Errorf("%w: object ID must be a positive integer", ErrInvalidDetailURL)
	}
	resolved.Fragment = ""
	resolved.RawFragment = ""
	return strconv.FormatUint(id, 10), resolved.String(), nil
}

func sameOrigin(left, right *url.URL) bool {
	return strings.EqualFold(left.Scheme, right.Scheme) &&
		strings.EqualFold(left.Hostname(), right.Hostname()) &&
		effectivePort(left) == effectivePort(right)
}

func effectivePort(parsed *url.URL) string {
	if port := parsed.Port(); port != "" {
		return port
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http":
		return "80"
	case "https":
		return "443"
	default:
		return ""
	}
}

func hasBizPrefix(urlPath string) bool {
	return urlPath == "/biz" || strings.HasPrefix(urlPath, "/biz/")
}

func attr(node *html.Node, name string) string {
	value, _ := getAttr(node, name)
	return value
}

func getAttr(node *html.Node, name string) (string, bool) {
	for _, attribute := range node.Attr {
		if attribute.Key == name {
			return attribute.Val, true
		}
	}
	return "", false
}

func hasClass(node *html.Node, class string) bool {
	for _, candidate := range strings.Fields(attr(node, "class")) {
		if candidate == class {
			return true
		}
	}
	return false
}

func walkElements(root *html.Node, visit func(*html.Node)) {
	if root.Type == html.ElementNode {
		visit(root)
	}
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		walkElements(child, visit)
	}
}

func directChild(root *html.Node, tag string) *html.Node {
	for child := root.FirstChild; child != nil; child = child.NextSibling {
		if child.Type == html.ElementNode && child.Data == tag {
			return child
		}
	}
	return nil
}

func nextSiblingElement(node *html.Node, tag string) *html.Node {
	for sibling := node.NextSibling; sibling != nil; sibling = sibling.NextSibling {
		if sibling.Type == html.ElementNode && sibling.Data == tag {
			return sibling
		}
	}
	return nil
}
