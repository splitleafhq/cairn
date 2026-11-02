package board

import (
	"bytes"
	_ "embed"
	"html/template"
	"time"

	"github.com/splitleafhq/cairn/internal/model"
)

// pageSource is the page, compiled into the binary. It is fixed: a run fills in data and changes nothing else. It
// loads and runs nothing, and its Content-Security-Policy says so.
//
//go:embed page.html
var pageSource string

var pageTemplate = template.Must(template.New("page.html").Parse(pageSource))

// pageData is what the page template is filled with.
type pageData struct {
	Tracker   string
	Generated string
	Total     int
	Columns   []pageColumn
}

type pageColumn struct {
	Status string
	Open   bool
	Count  int
	Cards  []pageCard
}

type pageCard struct {
	ID       string
	Title    string
	Tags     []string
	Updated  string // the time, in model.TimeLayout
	Day      string // its date
	Detailed string // its date and minute
}

// HTML is the board as one page, with every card of every column, collapsed ones included. tracker names the tracker
// and at is when the page was made, which it says, since it is a snapshot. The page is a document in full, and
// html/template escapes every title, tag and name in it.
func (b Board) HTML(tracker string, at time.Time) ([]byte, error) {
	data := pageData{Tracker: tracker, Generated: at.UTC().Format("2006-01-02 15:04 MST"), Total: b.Total()}
	for _, c := range b.Columns {
		col := pageColumn{Status: c.Status, Open: !c.Collapsed, Count: len(c.Issues)}
		for _, i := range c.Issues {
			u := i.Updated.UTC()
			col.Cards = append(col.Cards, pageCard{ID: i.ID, Title: i.Title, Tags: i.Tags, Updated: model.FormatTime(u),
				Day: u.Format(time.DateOnly), Detailed: u.Format("2006-01-02 15:04 MST")})
		}
		data.Columns = append(data.Columns, col)
	}
	var page bytes.Buffer
	if err := pageTemplate.Execute(&page, data); err != nil {
		return nil, err
	}
	return page.Bytes(), nil
}
