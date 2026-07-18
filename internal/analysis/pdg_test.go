package analysis

import (
	"strings"
	"testing"
)

func TestBuildGoPDG(t *testing.T) {
	res, err := BuildGoPDG("testdata/taintfix")
	if err != nil {
		t.Fatal(err)
	}

	// PDG: functions have CFG blocks recorded.
	var runPDG *FuncPDG
	for i := range res.Funcs {
		if strings.HasSuffix(res.Funcs[i].ID, ".run") {
			runPDG = &res.Funcs[i]
		}
	}
	if runPDG == nil || runPDG.Blocks == 0 {
		t.Fatalf("no PDG for run(): %+v", res.Funcs)
	}

	// Taint: env var → exec.Command flagged in run(), not in safe().
	var found *TaintFinding
	for i := range res.Taint {
		if res.Taint[i].Sink == "os/exec.Command" && strings.HasSuffix(res.Taint[i].Func, ".run") {
			found = &res.Taint[i]
		}
		if strings.HasSuffix(res.Taint[i].Func, ".safe") {
			t.Fatalf("false positive: taint reported in safe(): %+v", res.Taint[i])
		}
	}
	if found == nil {
		t.Fatalf("missing taint finding run() -> exec.Command; got %+v", res.Taint)
	}
	if found.Line == 0 {
		t.Error("taint finding has no line number")
	}
}
