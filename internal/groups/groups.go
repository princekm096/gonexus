// Package groups does cross-repo analysis over a repo group: it extracts each
// repo's public contract (exported symbols + HTTP routes) and links repos that
// share a contract key, enabling cross-repo blast radius.
//
// ponytail: links are by shared contract KEY (exported name or "METHOD path").
// Coarse but real for a Go backend + JS/TS consumers sharing type/route names;
// precise link resolution (import graph across module boundaries) is the
// upgrade path.
package groups

import (
	"fmt"
	"sort"
	"unicode"

	"github.com/yourorg/gonexus/internal/graph"
	"github.com/yourorg/gonexus/internal/registry"
)

// Contract is one public API element of a repo.
type Contract struct {
	Key  string `json:"key"`  // link key: symbol name, or "METHOD path"
	Kind string `json:"kind"` // "symbol" | "route"
	ID   string `json:"id"`   // node id (symbols) or route path
	Repo string `json:"repo"`
}

// Link is a contract key shared by two or more repos.
type Link struct {
	Key   string   `json:"key"`
	Repos []string `json:"repos"`
}

// SyncResult is the group's cross-repo contract registry.
type SyncResult struct {
	Group     string                `json:"group"`
	Contracts map[string][]Contract `json:"contracts"` // repo -> contracts
	Links     []Link                `json:"links"`
}

// contractsOf extracts a repo's public contract from its graph.
func contractsOf(g *graph.Graph, repo string) []Contract {
	var out []Contract
	for id, n := range g.Nodes {
		if isExported(n.Name) && isAPIKind(n.Kind) {
			out = append(out, Contract{Key: n.Name, Kind: "symbol", ID: id, Repo: repo})
		}
	}
	for _, r := range g.Routes {
		key := r.Method + " " + r.Path
		out = append(out, Contract{Key: key, Kind: "route", ID: r.Path, Repo: repo})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func isExported(name string) bool {
	if name == "" {
		return false
	}
	r := []rune(name)[0]
	return unicode.IsUpper(r)
}

func isAPIKind(k graph.Kind) bool {
	switch k {
	case graph.KindFunc, graph.KindMethod, graph.KindType, graph.KindInterface, graph.KindClass:
		return true
	}
	return false
}

// Sync builds the cross-repo contract registry for a group.
func Sync(store *registry.Store, group string) (*SyncResult, error) {
	f, err := registry.LoadGroups()
	if err != nil {
		return nil, err
	}
	grp, ok := f.Groups[group]
	if !ok {
		return nil, fmt.Errorf("no such group %q", group)
	}
	res := &SyncResult{Group: group, Contracts: map[string][]Contract{}}
	keyRepos := map[string]map[string]bool{} // key -> set of repos

	for _, repo := range grp.Repos {
		g, _, err := store.Graph(repo)
		if err != nil {
			continue // skip unloadable repo
		}
		cs := contractsOf(g, repo)
		res.Contracts[repo] = cs
		for _, c := range cs {
			if keyRepos[c.Key] == nil {
				keyRepos[c.Key] = map[string]bool{}
			}
			keyRepos[c.Key][repo] = true
		}
	}

	for key, repos := range keyRepos {
		if len(repos) < 2 {
			continue // shared by only one repo -> not a cross-repo link
		}
		rs := make([]string, 0, len(repos))
		for r := range repos {
			rs = append(rs, r)
		}
		sort.Strings(rs)
		res.Links = append(res.Links, Link{Key: key, Repos: rs})
	}
	sort.Slice(res.Links, func(i, j int) bool { return res.Links[i].Key < res.Links[j].Key })
	return res, nil
}

// CrossImpact returns, for a symbol in one repo, the other repos in the group
// that share its contract key (i.e. may be affected by a change to it).
func CrossImpact(store *registry.Store, group, repo, symbolID string) ([]string, string, error) {
	g, _, err := store.Graph(repo)
	if err != nil {
		return nil, "", err
	}
	n := g.Nodes[symbolID]
	if n == nil {
		return nil, "", fmt.Errorf("unknown symbol %q in repo %q", symbolID, repo)
	}
	key := n.Name
	sync, err := Sync(store, group)
	if err != nil {
		return nil, "", err
	}
	var affected []string
	for _, l := range sync.Links {
		if l.Key != key {
			continue
		}
		for _, r := range l.Repos {
			if r != repo {
				affected = append(affected, r)
			}
		}
	}
	return affected, key, nil
}
