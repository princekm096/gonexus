package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Group is a set of repos tracked together for cross-repo analysis.
type Group struct {
	Name  string   `json:"name"`
	Repos []string `json:"repos"`
}

type GroupsFile struct {
	Groups map[string]Group `json:"groups"`
}

// GroupsPath is ~/.gonexus/groups.json.
func GroupsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".gonexus", "groups.json"), nil
}

func LoadGroups() (*GroupsFile, error) {
	p, err := GroupsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if os.IsNotExist(err) {
		return &GroupsFile{Groups: map[string]Group{}}, nil
	}
	if err != nil {
		return nil, err
	}
	var f GroupsFile
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.Groups == nil {
		f.Groups = map[string]Group{}
	}
	return &f, nil
}

func (f *GroupsFile) Save() error {
	p, err := GroupsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

func (f *GroupsFile) Names() []string {
	out := make([]string, 0, len(f.Groups))
	for n := range f.Groups {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// GroupCreate makes an empty group (idempotent).
func GroupCreate(name string) error {
	f, err := LoadGroups()
	if err != nil {
		return err
	}
	if _, ok := f.Groups[name]; !ok {
		f.Groups[name] = Group{Name: name}
	}
	return f.Save()
}

// GroupAddRepo adds a repo to a group (group must exist; repo must be registered).
func GroupAddRepo(group, repo string) error {
	f, err := LoadGroups()
	if err != nil {
		return err
	}
	g, ok := f.Groups[group]
	if !ok {
		return fmt.Errorf("no such group %q", group)
	}
	reg, err := Load()
	if err != nil {
		return err
	}
	if _, ok := reg.Repos[repo]; !ok {
		return fmt.Errorf("repo %q is not registered", repo)
	}
	for _, r := range g.Repos {
		if r == repo {
			return f.Save() // already present
		}
	}
	g.Repos = append(g.Repos, repo)
	sort.Strings(g.Repos)
	f.Groups[group] = g
	return f.Save()
}

func GroupRemoveRepo(group, repo string) error {
	f, err := LoadGroups()
	if err != nil {
		return err
	}
	g, ok := f.Groups[group]
	if !ok {
		return fmt.Errorf("no such group %q", group)
	}
	kept := g.Repos[:0]
	for _, r := range g.Repos {
		if r != repo {
			kept = append(kept, r)
		}
	}
	g.Repos = kept
	f.Groups[group] = g
	return f.Save()
}

func GroupRemove(name string) error {
	f, err := LoadGroups()
	if err != nil {
		return err
	}
	delete(f.Groups, name)
	return f.Save()
}
