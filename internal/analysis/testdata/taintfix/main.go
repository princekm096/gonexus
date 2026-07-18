package main

import (
	"os"
	"os/exec"
)

// run has a source→sink flow: env var flows into a shell command.
func run() {
	cmd := os.Getenv("CMD")
	_ = exec.Command(cmd).Run()
}

// safe uses a literal, so no taint.
func safe() {
	_ = exec.Command("ls").Run()
}

func main() { run(); safe() }
