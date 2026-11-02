package model

import (
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/splitleafhq/cairn/internal/doc"
)

// reader reads typed values out of a file's fields. The first problem it meets is kept and returned by done, and
// every read after it gives the zero value, so a parser can read every key and check once.
type reader struct {
	fields []doc.Field
	read   map[string]bool
	err    error
}

func newReader(fields []doc.Field) *reader {
	return &reader{fields: fields, read: map[string]bool{}}
}

// field returns the field with key, checked to be a list or a scalar, and whether there is one.
func (r *reader) field(key string, list bool) (doc.Field, bool) {
	r.read[key] = true
	i := slices.IndexFunc(r.fields, func(f doc.Field) bool { return f.Key == key })
	if i < 0 || r.err != nil {
		return doc.Field{}, false
	}
	f := r.fields[i]
	switch {
	case list && !f.IsList:
		r.err = fmt.Errorf("model: %s must be a list", key)
	case !list && f.IsList:
		r.err = fmt.Errorf("model: %s must be a scalar, not a list", key)
	}
	return f, r.err == nil
}

// fail keeps err as the reader's problem unless it already has one.
func (r *reader) fail(err error) {
	if r.err == nil {
		r.err = err
	}
}

func (r *reader) text(key string) string {
	f, _ := r.field(key, false)
	return f.Value
}

// list returns the key's items, or def when the key is absent.
func (r *reader) list(key string, def []string) []string {
	if f, ok := r.field(key, true); ok {
		return f.List
	}
	return def
}

// date returns the key's value, which must be a DateLayout date when the key is present.
func (r *reader) date(key string) string {
	f, ok := r.field(key, false)
	if !ok {
		return ""
	}
	if _, err := time.Parse(DateLayout, f.Value); err != nil {
		r.fail(fmt.Errorf("model: bad %s date %q: %w", key, f.Value, err))
		return ""
	}
	return f.Value
}

// time returns the key's RFC 3339 time in UTC, or the zero time when the key is absent.
func (r *reader) time(key string) time.Time {
	f, ok := r.field(key, false)
	if !ok {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, f.Value)
	if err != nil {
		r.fail(fmt.Errorf("model: bad %s time %q: %w", key, f.Value, err))
	}
	return t.UTC()
}

// number returns the key's integer, or 0 when the key is absent.
func (r *reader) number(key string) int {
	f, ok := r.field(key, false)
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(f.Value)
	if err != nil {
		r.fail(fmt.Errorf("model: bad %s number %q: %w", key, f.Value, err))
	}
	return n
}

// How a boolean is written.
const (
	textTrue  = "true"
	textFalse = "false"
)

// boolean returns the key's value, which must be textTrue or textFalse when the key is present, or false when it is
// absent.
func (r *reader) boolean(key string) bool {
	f, ok := r.field(key, false)
	if !ok {
		return false
	}
	if f.Value != textTrue && f.Value != textFalse {
		r.fail(fmt.Errorf("model: bad %s %q: want %s or %s", key, f.Value, textTrue, textFalse))
	}
	return f.Value == textTrue
}

// float returns the key's number, or def when the key is absent.
func (r *reader) float(key string, def float64) float64 {
	f, ok := r.field(key, false)
	if !ok {
		return def
	}
	v, err := strconv.ParseFloat(f.Value, 64)
	if err != nil {
		r.fail(fmt.Errorf("model: bad %s %q: %w", key, f.Value, err))
	}
	return v
}

// done returns the first problem met so far, else an error for the first of required that is absent or empty.
func (r *reader) done(required ...string) error {
	if r.err != nil {
		return r.err
	}
	for _, key := range required {
		if i := slices.IndexFunc(r.fields, func(f doc.Field) bool { return f.Key == key }); i < 0 ||
			r.fields[i].Value == "" {
			return fmt.Errorf("model: missing required key %q", key)
		}
	}
	return nil
}

// extra returns the fields no read asked for, in file order, or nil when there are none.
func (r *reader) extra() []doc.Field {
	var extra []doc.Field
	for _, f := range r.fields {
		if !r.read[f.Key] {
			extra = append(extra, f)
		}
	}
	return extra
}

// writer builds a file's fields in order. The opt methods leave out an empty value.
type writer []doc.Field

func (w *writer) text(key, v string) {
	*w = append(*w, doc.Field{Key: key, Value: v})
}

func (w *writer) optText(key, v string) {
	if v != "" {
		w.text(key, v)
	}
}

func (w *writer) list(key string, items []string) {
	*w = append(*w, doc.Field{Key: key, List: items, IsList: true})
}

func (w *writer) optList(key string, items []string) {
	if len(items) > 0 {
		w.list(key, items)
	}
}

func (w *writer) time(key string, t time.Time) {
	w.text(key, FormatTime(t))
}

func (w *writer) optTime(key string, t time.Time) {
	if !t.IsZero() {
		w.time(key, t)
	}
}

func (w *writer) optNumber(key string, n int) {
	if n != 0 {
		w.text(key, strconv.Itoa(n))
	}
}

// optTrue writes textTrue when v is true, and nothing when it is false.
func (w *writer) optTrue(key string, v bool) {
	if v {
		w.text(key, textTrue)
	}
}

func (w *writer) float(key string, v float64) {
	w.text(key, strconv.FormatFloat(v, 'g', -1, 64))
}

// with returns the fields written so far, then extra.
func (w writer) with(extra []doc.Field) []doc.Field {
	return append(w, extra...)
}
