package graph

import (
	"sort"
	"strings"
)

// knownFrameworks maps an import-path substring to a framework label.
// ponytail: substring match on import paths — cheap and good enough; extend the
// map as needed rather than adding per-language detectors.
var knownFrameworks = map[string]string{
	"gin-gonic/gin":       "Gin",
	"labstack/echo":       "Echo",
	"go-chi/chi":          "Chi",
	"gorilla/mux":         "Gorilla Mux",
	"gofiber/fiber":       "Fiber",
	"gorm.io":             "GORM",
	"jmoiron/sqlx":        "sqlx",
	"google.golang.org/grpc": "gRPC",
	"connectrpc.com":      "Connect",
	"spf13/cobra":         "Cobra",
	"urfave/cli":          "urfave/cli",
	"spf13/viper":         "Viper",
	"stretchr/testify":    "Testify",
	// JS/TS
	"vue":     "Vue",
	"react":   "React",
	"express": "Express",
	"next":    "Next.js",
	"@nestjs": "NestJS",
	"svelte":  "Svelte",
	"axios":   "Axios",
}

// Frameworks returns the frameworks detected from the graph's import edges,
// sorted. Derived from existing `imports` edges — no extra indexing.
func Frameworks(g *Graph) []string {
	seen := map[string]bool{}
	for _, e := range g.Edges {
		if e.Kind != EdgeImports {
			continue
		}
		for sub, label := range knownFrameworks {
			if strings.Contains(e.To, sub) {
				seen[label] = true
			}
		}
	}
	out := make([]string, 0, len(seen))
	for f := range seen {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
