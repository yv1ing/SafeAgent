package console

import (
	"strconv"
	"time"
)

const (
	DefaultPageSize = 50
	MaximumPageSize = 100
)

// Cursor is the keyset position of a newest-first listing; the zero value starts at the newest row.
// The repository's own page cap is above MaximumPageSize so a listing can fetch one row more than
// it returns.
type Cursor struct {
	Before   time.Time
	BeforeID string
	Limit    int
}

// ParseCursor validates the raw limit, before and before_id query values.
func ParseCursor(limit, before, beforeID string) (Cursor, error) {
	cursor := Cursor{Limit: DefaultPageSize}
	if limit != "" {
		parsed, err := strconv.Atoi(limit)
		if err != nil || parsed < 1 || parsed > MaximumPageSize {
			return cursor, Invalid("invalid_limit")
		}
		cursor.Limit = parsed
	}
	if before == "" && beforeID == "" {
		return cursor, nil
	}
	if before == "" || beforeID == "" {
		return cursor, Invalid("invalid_cursor")
	}
	parsed, err := time.Parse(time.RFC3339Nano, before)
	if err != nil {
		return cursor, Invalid("invalid_cursor")
	}
	cursor.Before, cursor.BeforeID = parsed.UTC(), beforeID
	return cursor, nil
}

// Fetch is the number of rows a listing must request to learn whether another page follows.
func (cursor Cursor) Fetch() int { return cursor.Limit + 1 }

// Position is the sort key of one listed row.
type Position struct {
	At time.Time
	ID string
}

// Page is one listing page; Next is nil on the last page.
type Page[Row any] struct {
	Items []Row
	Next  *Position
}

// Paginate trims rows fetched with Cursor.Fetch to one page and derives the next position.
func Paginate[Row any](rows []Row, cursor Cursor, position func(Row) Position) Page[Row] {
	if len(rows) <= cursor.Limit {
		return Page[Row]{Items: rows}
	}
	items := rows[:cursor.Limit]
	next := position(items[len(items)-1])
	return Page[Row]{Items: items, Next: &next}
}
