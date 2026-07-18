package analysis

import (
	"encoding/json"
	"os"
)

// Save writes the PDG result to path (JSON).
func (r *Result) Save(path string) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// Load reads a PDG result; returns an empty result if the file is absent.
func Load(path string) (*Result, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Result{}, nil
	}
	if err != nil {
		return nil, err
	}
	var r Result
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// FindByFunc returns the PDG for a function id, or nil.
func (r *Result) FindByFunc(id string) *FuncPDG {
	for i := range r.Funcs {
		if r.Funcs[i].ID == id {
			return &r.Funcs[i]
		}
	}
	return nil
}

// TaintForFunc returns taint findings for a function id (or all if id is empty).
func (r *Result) TaintForFunc(id string) []TaintFinding {
	if id == "" {
		return r.Taint
	}
	var out []TaintFinding
	for _, t := range r.Taint {
		if t.Func == id {
			out = append(out, t)
		}
	}
	return out
}
