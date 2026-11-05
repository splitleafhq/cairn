package cli

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
)

// embedServer stands in for an OpenAI-compatible /v1/embeddings endpoint. It answers with the vector listed for
// each input text, and with one that is at right angles to every listed vector (so it scores 0) for any other text.
// Its answers come back in reverse order, each with its "index", as the API allows.
type embedServer struct {
	*httptest.Server
	mu       sync.Mutex
	vectors  map[string][]float64
	requests int
	inputs   int
	models   []string
	status   int    // when not 0, the status every request gets
	reply    string // when not "", the body of a 200 reply
}

func newEmbedServer(t *testing.T, vectors map[string][]float64) *embedServer {
	t.Helper()
	e := &embedServer{vectors: vectors}
	e.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Input []string `json:"input"`
			Model string   `json:"model"`
		}
		if r.Method != http.MethodPost || r.URL.Path != "/v1/embeddings" || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		e.requests++
		e.inputs += len(req.Input)
		e.models = append(e.models, req.Model)
		switch {
		case e.status != 0:
			http.Error(w, "unavailable", e.status)
			return
		case e.reply != "":
			fmt.Fprint(w, e.reply)
			return
		}
		type item struct {
			Index     int       `json:"index"`
			Embedding []float64 `json:"embedding"`
		}
		var data []item
		for i, text := range slices.Backward(req.Input) {
			v, ok := e.vectors[text]
			if !ok {
				v = []float64{0, 0, 1}
			}
			data = append(data, item{i, v})
		}
		json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(e.Close)
	return e
}

func (e *embedServer) url() string { return e.URL + "/v1/embeddings" }

func (e *embedServer) counts() (requests, inputs int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.requests, e.inputs
}

// setConfig replaces config.yml with the statuses a test needs, plus the extra lines.
func (tr *tracker) setConfig(extra ...string) {
	tr.t.Helper()
	src := "---\nstatuses: [backlog, open, in-progress, done, archived]\n" + strings.Join(extra, "") + "---\n"
	if err := os.WriteFile(filepath.Join(tr.dir, ".cairn", "config.yml"), []byte(src), 0o644); err != nil {
		tr.t.Fatal(err)
	}
}

// seed files an issue with the title, then gives it the status when it is not the first one.
func (tr *tracker) seed(title, status string) string {
	tr.t.Helper()
	i := tr.issue("new", title)
	if status != "" {
		tr.issue("edit", i.ID, "--status", status)
	}
	return i.ID
}

// filed runs new with --json and returns the exit code, the issue, the warnings, and stderr. It checks that stderr
// holds each warning, in order, as the contract says.
func (tr *tracker) filed(title string) (int, issue, []string, string) {
	tr.t.Helper()
	code, out, errOut := tr.run("new", title, "--json")
	var e struct {
		OK       bool     `json:"ok"`
		Data     issue    `json:"data"`
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &e); err != nil {
		tr.t.Fatalf("new %q: stdout is not one JSON envelope: %q", title, out)
	}
	var want string
	for _, w := range e.Warnings {
		want += "cairn: warning: " + w + "\n"
	}
	if errOut != want {
		tr.t.Fatalf("new %q: stderr %q, want the warnings as lines: %q", title, errOut, want)
	}
	return code, e.Data, e.Warnings, errOut
}

// actionLine is the warning that follows the list, for the new issue id.
func actionLine(id string) string {
	return fmt.Sprintf("if %[1]s is the same work as one of these: put what %[1]s adds on that issue as evidence, then run: cairn rm %[1]s", id)
}

func similarLine(id, method string, score float64, title string) string {
	return fmt.Sprintf("similar issue %s (%s %.2f): %s", id, method, score, title)
}

// The nearest titles by meaning come out as warnings, best first, at most 3, and closed issues count but archived
// ones do not. The title gets the configured prompt before it is embedded, and the model is sent.
func TestNewListsSimilarByMeaning(t *testing.T) {
	const prompt = "test-prompt: "
	vec := func(title string, v ...float64) (string, []float64) { return prompt + title, v }
	vectors := map[string][]float64{}
	for title, v := range map[string][]float64{
		"test-export drops final row": {1, 0, 0},
		"test-regression in export":   {0.96, 0.28, 0},
		"test-export loses last row":  {0.8, 0.6, 0},
		"test-import page crashes":    {0.6, 0.8, 0},
		"test-docs typo":              {0.28, 0.96, 0},
		"test-old export":             {1, 0, 0},
	} {
		k, val := vec(title, v...)
		vectors[k] = val
	}
	srv := newEmbedServer(t, vectors)
	tr := newTracker(t)
	tr.setConfig()
	done := tr.seed("test-regression in export", "done")
	near := tr.seed("test-export loses last row", "")
	mid := tr.seed("test-import page crashes", "open")
	tr.seed("test-docs typo", "")
	tr.seed("test-unrelated", "")
	tr.seed("test-old export", "archived")
	tr.setConfig("embeddings_url: "+srv.url()+"\n", "embeddings_model: test-model\n",
		`embeddings_prompt: "`+prompt+`"`+"\n")

	code, got, warnings, _ := tr.filed("test-export drops final row")
	if code != 0 || got.Title != "test-export drops final row" {
		t.Fatalf("exit %d, issue %+v", code, got)
	}
	want := []string{
		similarLine(done, "title meaning", 0.96, "test-regression in export"),
		similarLine(near, "title meaning", 0.80, "test-export loses last row"),
		similarLine(mid, "title meaning", 0.60, "test-import page crashes"),
		actionLine(got.ID),
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings\ngot  %q\nwant %q", warnings, want)
	}
	if shown := tr.issue("show", got.ID); shown.Title != got.Title {
		t.Fatalf("the issue was not filed: %+v", shown)
	}
	if !slices.Equal(srv.models, []string{"test-model"}) {
		t.Errorf("models sent: %q", srv.models)
	}
}

// Without embeddings_url the same titles are ranked by word overlap, best first, ties by id, at most 3, and a title
// that shares no word is left out.
func TestNewListsSimilarByWords(t *testing.T) {
	tr := newTracker(t)
	tr.setConfig()
	exact := tr.seed("Export drops the last row", "")
	reworded := tr.seed("Export loses the last row", "done")
	short := tr.seed("the row", "")
	tr.seed("Add a test for the import", "")
	tr.seed("Unrelated thing", "")
	tr.seed("Export drops the last row, archived", "archived")

	code, got, warnings, _ := tr.filed("Export drops the final row")
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	want := []string{
		similarLine(exact, "word overlap", 0.67, "Export drops the last row"),
		similarLine(reworded, "word overlap", 0.43, "Export loses the last row"),
		similarLine(short, "word overlap", 0.40, "the row"),
		actionLine(got.ID),
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings\ngot  %q\nwant %q", warnings, want)
	}
}

func TestNewSimilarTiesGoByID(t *testing.T) {
	tr := newTracker(t)
	a, b := tr.seed("test-twin title", ""), tr.seed("test-twin title", "")
	_, got, warnings, _ := tr.filed("test-twin title again")
	ids := []string{a, b}
	slices.Sort(ids)
	want := []string{
		similarLine(ids[0], "word overlap", 0.75, "test-twin title"),
		similarLine(ids[1], "word overlap", 0.75, "test-twin title"),
		actionLine(got.ID),
	}
	if !reflect.DeepEqual(warnings, want) {
		t.Fatalf("warnings\ngot  %q\nwant %q", warnings, want)
	}
}

func TestNewWithNothingSimilarIsQuiet(t *testing.T) {
	tr := newTracker(t)
	tr.seed("test-alpha", "")
	if code, _, warnings, _ := tr.filed("beta"); code != 0 || len(warnings) != 0 {
		t.Fatalf("exit %d, warnings %q", code, warnings)
	}
}

// When the endpoint is set but fails, the issue is still filed, one warning says why, and the titles are ranked by
// word overlap.
func TestNewFallsBackWhenEmbeddingsFail(t *testing.T) {
	for name, set := range map[string]func(*embedServer){
		"non-200":      func(e *embedServer) { e.status = http.StatusInternalServerError },
		"bad JSON":     func(e *embedServer) { e.reply = "not json" },
		"no vectors":   func(e *embedServer) { e.reply = `{"data": []}` },
		"unreachable":  func(e *embedServer) { e.Close() },
		"empty vector": func(e *embedServer) { e.reply = `{"data": [{"index": 0, "embedding": []}]}` },
	} {
		t.Run(name, func(t *testing.T) {
			srv := newEmbedServer(t, nil)
			set(srv)
			tr := newTracker(t)
			tr.setConfig("embeddings_url: " + srv.url() + "\n")
			id := tr.seed("Export drops the last row", "")

			code, got, warnings, _ := tr.filed("Export drops the final row")
			if code != 0 || got.ID == "" {
				t.Fatalf("exit %d, issue %+v", code, got)
			}
			if len(warnings) != 3 || !strings.HasPrefix(warnings[0], "embeddings unavailable") ||
				warnings[1] != similarLine(id, "word overlap", 0.67, "Export drops the last row") ||
				warnings[2] != actionLine(got.ID) {
				t.Fatalf("warnings %q", warnings)
			}
		})
	}
}

// Vectors are kept in .cairn/.index, so a second new asks only for the new title; a changed setting asks for every
// title again; and a damaged cache is fetched again without a complaint.
func TestNewCachesTitleVectors(t *testing.T) {
	vectors := map[string][]float64{"test-new one": {1, 0, 0}, "test-seed a": {0.8, 0.6, 0}}
	srv := newEmbedServer(t, vectors)
	tr := newTracker(t)
	tr.setConfig()
	tr.seed("test-seed a", "")
	b := tr.seed("test-seed b", "")
	tr.seed("test-seed c", "")
	tr.setConfig("embeddings_url: " + srv.url() + "\n")

	var last int
	step := func(title string) (inputs int, warnings []string) {
		t.Helper()
		code, _, warnings, _ := tr.filed(title)
		if code != 0 {
			t.Fatalf("exit %d", code)
		}
		_, total := srv.counts()
		inputs, last = total-last, total
		return inputs, warnings
	}
	if inputs, _ := step("test-new one"); inputs != 4 {
		t.Fatalf("first new embedded %d titles, want the 3 others and the new one", inputs)
	}
	if inputs, _ := step("test-new two"); inputs != 1 {
		t.Fatalf("second new embedded %d titles, want only its own", inputs)
	}

	if code, _, errOut := tr.run("edit", b, "--title", "test-seed renamed"); code != 0 {
		t.Fatalf("edit --title: exit %d, %s", code, errOut)
	}
	_, last = srv.counts()
	if inputs, _ := step("test-new after edit"); inputs != 1 {
		t.Fatalf("after an edit --title, new embedded %d titles, want only its own", inputs)
	}

	tr.setConfig("embeddings_url: "+srv.url()+"\n", `embeddings_prompt: "test-changed: "`+"\n")
	if inputs, _ := step("test-new three"); inputs != 7 {
		t.Fatalf("after a changed prompt, new embedded %d titles, want all 6 others and its own", inputs)
	}

	err := filepath.WalkDir(filepath.Join(tr.dir, ".cairn", ".index"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			err = os.WriteFile(p, []byte("not a vector"), 0o644)
		}
		return err
	})
	if err != nil {
		t.Fatalf("no cache under .cairn/.index: %v", err)
	}
	inputs, warnings := step("test-new four")
	if inputs != 8 {
		t.Fatalf("with a damaged cache, new embedded %d titles, want all 7 others and its own", inputs)
	}
	for _, w := range warnings {
		if strings.HasPrefix(w, "embeddings unavailable") {
			t.Errorf("a damaged cache must not be an error: %q", warnings)
		}
	}
}

// Nothing in the check may make new fail: not a dead endpoint with an issue that cannot be read, and not a cache
// that cannot be written.
func TestNewStillFilesWhenTheCheckFails(t *testing.T) {
	t.Run("endpoint down and an unreadable issue", func(t *testing.T) {
		srv := newEmbedServer(t, nil)
		srv.status = http.StatusBadGateway
		tr := newTracker(t)
		tr.setConfig("embeddings_url: " + srv.url() + "\n")
		broken := tr.seed("test-broken", "")
		bad := filepath.Join(tr.dir, ".cairn", "issues", broken, "issue.md")
		if err := os.WriteFile(bad, []byte("not frontmatter"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, got, warnings, _ := tr.filed("test-still filed")
		if code != 0 || len(warnings) == 0 {
			t.Fatalf("exit %d, warnings %q", code, warnings)
		}
		if _, err := os.Stat(filepath.Join(tr.dir, ".cairn", "issues", got.ID, "issue.md")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("cache cannot be written", func(t *testing.T) {
		srv := newEmbedServer(t, map[string][]float64{"test-title": {1, 0, 0}, "test-seed": {0.8, 0.6, 0}})
		tr := newTracker(t)
		tr.setConfig("embeddings_url: " + srv.url() + "\n")
		id := tr.seed("test-seed", "")
		if err := os.WriteFile(filepath.Join(tr.dir, ".cairn", ".index"), []byte("a file"), 0o644); err != nil {
			t.Fatal(err)
		}
		code, _, warnings, _ := tr.filed("test-title")
		if code != 0 || len(warnings) != 3 {
			t.Fatalf("exit %d, warnings %q", code, warnings)
		}
		if want := similarLine(id, "title meaning", 0.80, "test-seed"); !slices.Contains(warnings, want) {
			t.Errorf("warnings %q lack %q", warnings, want)
		}
	})
}
