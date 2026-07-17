// Package embed produces text embeddings via an OpenAI-compatible HTTP endpoint
// (OpenAI, Ollama's /v1/embeddings, LM Studio, etc.). It's optional: if
// GONEXUS_EMBED_URL is unset, FromEnv returns nil and search stays BM25-only.
//
// ponytail: one protocol (OpenAI /v1/embeddings) covers the common local +
// hosted providers. No SDK, just net/http.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

// Embedder turns texts into vectors.
type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// FromEnv builds an Embedder from environment config, or nil if not configured.
//
//	GONEXUS_EMBED_URL    full endpoint, e.g. http://localhost:11434/v1/embeddings
//	GONEXUS_EMBED_MODEL  model name, e.g. nomic-embed-text
//	GONEXUS_EMBED_KEY    optional Bearer token
func FromEnv() Embedder {
	url := os.Getenv("GONEXUS_EMBED_URL")
	if url == "" {
		return nil
	}
	return &httpEmbedder{
		url:    url,
		model:  os.Getenv("GONEXUS_EMBED_MODEL"),
		key:    os.Getenv("GONEXUS_EMBED_KEY"),
		client: &http.Client{Timeout: 60 * time.Second},
		batch:  100,
	}
}

type httpEmbedder struct {
	url    string
	model  string
	key    string
	client *http.Client
	batch  int
}

type embedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (e *httpEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	out := make([][]float32, 0, len(texts))
	for i := 0; i < len(texts); i += e.batch {
		end := i + e.batch
		if end > len(texts) {
			end = len(texts)
		}
		vecs, err := e.embedChunk(ctx, texts[i:end])
		if err != nil {
			return nil, err
		}
		out = append(out, vecs...)
	}
	return out, nil
}

func (e *httpEmbedder) embedChunk(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedReq{Model: e.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if e.key != "" {
		req.Header.Set("Authorization", "Bearer "+e.key)
	}
	resp, err := e.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embed: %s", resp.Status)
	}
	var out embedResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embed: got %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(out.Data))
	for i, d := range out.Data {
		vecs[i] = d.Embedding
	}
	return vecs, nil
}
