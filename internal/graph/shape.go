package graph

import (
	"sort"
	"strings"
)

// ShapeFinding is a consumer property access that isn't in the matched
// provider type's field set — a likely typo or a stale/removed field.
type ShapeFinding struct {
	Object  string   `json:"object"`  // consumer object identifier
	Type    string   `json:"type"`    // matched provider type (by name)
	File    string   `json:"file"`    // consumer file
	Unknown []string `json:"unknown"` // accessed props not on the provider type
}

// ShapeCheck validates recorded consumer access shapes against provider type
// field shapes, matching consumer object identifiers to types by name.
//
// ponytail: matches by name (case-insensitive) — the honest heuristic for
// cross-language shape validation without full type-flow. Precise matching
// (which API call produced the object) is the upgrade path.
func (g *Graph) ShapeCheck() []ShapeFinding {
	fields := map[string]map[string]bool{} // lower type name -> lower field set
	for _, n := range g.Nodes {
		if len(n.Fields) == 0 {
			continue
		}
		set := make(map[string]bool, len(n.Fields))
		for _, f := range n.Fields {
			set[strings.ToLower(f)] = true
		}
		fields[strings.ToLower(n.Name)] = set
	}

	var out []ShapeFinding
	for _, s := range g.Shapes {
		set, ok := fields[strings.ToLower(s.Object)]
		if !ok {
			continue // consumer object doesn't name a known provider type
		}
		var unknown []string
		for _, p := range s.Props {
			if !set[strings.ToLower(p)] {
				unknown = append(unknown, p)
			}
		}
		if len(unknown) > 0 {
			sort.Strings(unknown)
			out = append(out, ShapeFinding{Object: s.Object, Type: s.Object, File: s.File, Unknown: unknown})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Object != out[j].Object {
			return out[i].Object < out[j].Object
		}
		return out[i].File < out[j].File
	})
	return out
}
