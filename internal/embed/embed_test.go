package embed

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFromEnvUnset(t *testing.T) {
	t.Setenv("GONEXUS_EMBED_URL", "")
	if FromEnv() != nil {
		t.Fatal("FromEnv should be nil without GONEXUS_EMBED_URL")
	}
}

func TestHTTPEmbedder(t *testing.T) {
	var gotAuth string
	var gotReq embedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotReq)
		data := make([]map[string]any, 0, len(gotReq.Input))
		for i := range gotReq.Input {
			data = append(data, map[string]any{"embedding": []float32{float32(i), 1, 2}})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	defer srv.Close()

	t.Setenv("GONEXUS_EMBED_URL", srv.URL)
	t.Setenv("GONEXUS_EMBED_MODEL", "test-model")
	t.Setenv("GONEXUS_EMBED_KEY", "secret")

	e := FromEnv()
	if e == nil {
		t.Fatal("FromEnv nil despite URL set")
	}
	vecs, err := e.Embed(context.Background(), []string{"alpha", "beta"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 || len(vecs[1]) != 3 || vecs[1][0] != 1 {
		t.Fatalf("vecs = %v, want 2 vectors of dim 3", vecs)
	}
	if gotReq.Model != "test-model" {
		t.Fatalf("model = %q, want test-model", gotReq.Model)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("auth = %q, want Bearer secret", gotAuth)
	}
}
