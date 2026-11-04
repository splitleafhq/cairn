package similar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/splitleafhq/cairn/internal/store"
)

const (
	// Timeout is how long ByMeaning may spend on the endpoint, all requests together.
	Timeout = 10 * time.Second

	// batchSize is the most titles sent in one request.
	batchSize = 64

	// maxReply is the most bytes of a reply that are read.
	maxReply = 64 << 20

	cacheDir  = "titles" // under the tracker's .index folder
	cacheFile = ".json"
)

// Embedder is an OpenAI-compatible /v1/embeddings endpoint, and where it keeps the vectors it has fetched.
type Embedder struct {
	URL    string
	Model  string // sent as "model" when not empty
	Prompt string // put before every title before it is embedded
	Index  string // the tracker's .index folder
}

// ByMeaning ranks the candidates by the cosine similarity of their title vectors with title's. A vector comes from
// the cache when e has it, else from the endpoint, in batches, and is cached once fetched. The error is why the
// endpoint could not rank them. cacheErr, which can come with hits, is why a vector could not be kept.
func (e Embedder) ByMeaning(ctx context.Context, title string, candidates []Candidate) (hits []Hit, cacheErr, err error) {
	titles := make([]string, 0, len(candidates)+1)
	titles = append(titles, title)
	for _, c := range candidates {
		titles = append(titles, c.Title)
	}
	vectors, cacheErr, err := e.vectors(ctx, titles)
	if err != nil {
		return nil, nil, err
	}
	for i, c := range candidates {
		if len(vectors[i+1]) != len(vectors[0]) {
			return nil, nil, errors.New("vectors differ in size")
		}
		hits = append(hits, Hit{c, cosine(vectors[0], vectors[i+1])})
	}
	return Rank(hits), cacheErr, nil
}

// vectors returns one vector per title, in order.
func (e Embedder) vectors(ctx context.Context, titles []string) (vectors [][]float32, cacheErr, err error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	vectors = make([][]float32, len(titles))
	var missing []int
	for i, t := range titles {
		if v := e.cached(t); v != nil {
			vectors[i] = v
		} else {
			missing = append(missing, i)
		}
	}
	for len(missing) > 0 {
		batch := missing[:min(len(missing), batchSize)]
		missing = missing[len(batch):]
		input := make([]string, len(batch))
		for j, i := range batch {
			input[j] = e.Prompt + titles[i]
		}
		got, err := e.fetch(ctx, input)
		if err != nil {
			return nil, nil, err
		}
		for j, i := range batch {
			vectors[i] = got[j]
			if err := e.keep(titles[i], got[j]); err != nil && cacheErr == nil {
				cacheErr = err
			}
		}
	}
	return vectors, cacheErr, nil
}

// fetch asks the endpoint for the vectors of input, one each and in order.
func (e Embedder) fetch(ctx context.Context, input []string) ([][]float32, error) {
	body, err := json.Marshal(struct {
		Input []string `json:"input"`
		Model string   `json:"model,omitempty"`
	}{input, e.Model})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.URL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s answered %s", e.URL, resp.Status)
	}
	var reply struct {
		Data []struct {
			Index     *int      `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxReply)).Decode(&reply); err != nil {
		return nil, fmt.Errorf("bad reply: %w", err)
	}
	if len(reply.Data) != len(input) {
		return nil, fmt.Errorf("bad reply: %d vectors for %d titles", len(reply.Data), len(input))
	}
	vectors := make([][]float32, len(input))
	for pos, d := range reply.Data {
		i := pos
		if d.Index != nil {
			i = *d.Index
		}
		if i < 0 || i >= len(input) || vectors[i] != nil || len(d.Embedding) == 0 {
			return nil, errors.New("bad reply: an index or a vector is wrong")
		}
		vectors[i] = d.Embedding
	}
	return vectors, nil
}

// path is the cache file of title's vector. Its name is a hash of everything the vector depends on.
func (e Embedder) path(title string) string {
	h := sha256.New()
	for _, part := range []string{e.URL, e.Model, e.Prompt, title} {
		fmt.Fprintf(h, "%d:%s", len(part), part)
	}
	return filepath.Join(e.Index, cacheDir, hex.EncodeToString(h.Sum(nil))+cacheFile)
}

// cached is title's vector from the cache, or nil when it has none or the file is damaged.
func (e Embedder) cached(title string) []float32 {
	data, err := os.ReadFile(e.path(title))
	if err != nil {
		return nil
	}
	var v []float32
	if json.Unmarshal(data, &v) != nil || len(v) == 0 {
		return nil
	}
	return v
}

// keep writes title's vector to the cache.
func (e Embedder) keep(title string, v []float32) error {
	p := e.path(title)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return store.WriteFileAtomic(p, data, 0o644)
}

// cosine is the cosine similarity of a and b, which have the same length. It is 0 when either has no length.
func cosine(a, b []float32) float64 {
	var dot, na, nb float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
