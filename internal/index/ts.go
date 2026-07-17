package index

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/yourorg/gonexus/internal/graph"
)

// tsPayload is what tools/ts-extractor/extract.mjs prints.
type tsPayload struct {
	Nodes []struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		Name    string `json:"name"`
		Package string `json:"package"`
		File    string `json:"file"`
		Line    int    `json:"line"`
	} `json:"nodes"`
	Edges []struct {
		From string `json:"from"`
		To   string `json:"to"`
		Kind string `json:"kind"`
	} `json:"edges"`
}

// extractorPath finds the Node extractor script. Override with
// GONEXUS_TS_EXTRACTOR; default is relative to the working dir (dev layout).
func extractorPath() string {
	if p := os.Getenv("GONEXUS_TS_EXTRACTOR"); p != "" {
		return p
	}
	return filepath.Join("tools", "ts-extractor", "extract.mjs")
}

// BuildTS indexes TS/JS/Vue under dir by shelling out to the Node extractor.
// Requires `node` and `npm install` in tools/ts-extractor.
func BuildTS(dir string) (*graph.Graph, error) {
	script := extractorPath()
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("ts extractor not found at %s (set GONEXUS_TS_EXTRACTOR): %w", script, err)
	}
	cmd := exec.Command("node", script, dir)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ts extractor: %w", err)
	}
	var p tsPayload
	if err := json.Unmarshal(out, &p); err != nil {
		return nil, fmt.Errorf("ts extractor output: %w", err)
	}
	g := graph.New()
	for _, n := range p.Nodes {
		g.AddNode(&graph.Node{
			ID: n.ID, Kind: graph.Kind(n.Kind), Name: n.Name,
			Package: n.Package, File: n.File, Line: n.Line,
		})
	}
	for _, e := range p.Edges {
		g.AddEdge(graph.Edge{From: e.From, To: e.To, Kind: graph.EdgeKind(e.Kind)})
	}
	return g, nil
}

func hasGo(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	return err == nil
}
