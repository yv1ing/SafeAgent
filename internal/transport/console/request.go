package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	service "github.com/yv1ing/SafeAgent/internal/service/console"
)

const maximumBodyBytes = 1 << 20

// decode reads one JSON object into target and refuses unknown fields, trailing data and bodies
// above maximumBodyBytes.
func decode(ctx *gin.Context, target any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(ctx.Writer, ctx.Request.Body, maximumBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return errInvalidRequest
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errInvalidRequest
	}
	return nil
}

type pageQuery struct {
	Before   string `query:"before" json:"-" description:"RFC 3339 time of the last row of the previous page"`
	BeforeID string `query:"before_id" json:"-" description:"ID of the last row of the previous page"`
	Limit    int    `query:"limit" json:"-" minimum:"1" maximum:"100"`
}

func cursor(ctx *gin.Context) (service.Cursor, error) {
	return service.ParseCursor(ctx.Query("limit"), ctx.Query("before"), ctx.Query("before_id"))
}

// CursorView is where the next page starts; pass it back as before and before_id.
type CursorView struct {
	Before   string `json:"before" required:"true"`
	BeforeID string `json:"before_id" required:"true"`
}

func cursorView(position *service.Position) *CursorView {
	if position == nil {
		return nil
	}
	return &CursorView{Before: stamp(position.At), BeforeID: position.ID}
}

// stamp renders a timestamp as RFC 3339 with nanoseconds in UTC; a zero time is the empty string.
func stamp(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func stampOptional(value *time.Time) string {
	if value == nil {
		return ""
	}
	return stamp(*value)
}

func nonNil[Item any](values []Item) []Item {
	if values == nil {
		return []Item{}
	}
	return values
}

func viewAll[Source, View any](sources []Source, view func(*Source) View) []View {
	views := make([]View, 0, len(sources))
	for index := range sources {
		views = append(views, view(&sources[index]))
	}
	return views
}

type projectPath struct {
	ProjectID string `path:"project_id" json:"-" required:"true"`
}

func pathInt(value string) (int64, error) {
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, errInvalidRequest
	}
	return parsed, nil
}
