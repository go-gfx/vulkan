package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const registry = "../../registry/vk.xml.gz"

func run(args ...string) (string, error) {
	cmd := RootCmd()
	var out, errb bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errb)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

func TestWriteThenCheck(t *testing.T) {
	dir := t.TempDir()
	out, err := run("--registry", registry, "--out", dir, "--package", "vk")
	if err != nil || !strings.Contains(out, "types_gen.go") || !strings.Contains(out, "layout_gen_test.go") {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := run("-r", registry, "-o", dir, "--check"); err != nil {
		t.Fatalf("a fresh output failed --check: %v", err)
	}
	// The committed files are current too.
	if _, err := run("-r", registry, "-o", "../../internal/vk", "--check"); err != nil {
		t.Fatalf("internal/vk is out of date: %v", err)
	}
	// Break check: a stale file is refused, and --check writes nothing.
	path := filepath.Join(dir, "types_gen.go")
	if err := os.WriteFile(path, []byte("package vk\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := run("-r", registry, "-o", dir, "--check"); err == nil || !strings.Contains(err.Error(), "out of date") {
		t.Fatalf("stale file: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "package vk\n" {
		t.Fatal("--check wrote a file")
	}
}

func TestErrors(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.xml")
	if err := os.WriteFile(bad, []byte("<nope/>"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"no registry file": {"-r", filepath.Join(dir, "missing.xml"), "-o", dir},
		"bad registry":     {"-r", bad, "-o", dir},
		"bad package":      {"-r", registry, "-o", dir, "--package", "not a package"},
		"output not a dir": {"-r", registry, "-o", filepath.Join(dir, "missing", "dir")},
		"check, no files":  {"-r", registry, "-o", dir, "--check"},
		"positional args":  {"extra"},
		"unknown flag":     {"--nope"},
	} {
		if _, err := run(args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestMainExit(t *testing.T) {
	var code int
	osExit = func(c int) { code = c }
	defer func() { osExit = os.Exit }()
	old := os.Args
	defer func() { os.Args = old }()
	os.Args = []string{"vkgen", "--nope"}
	main()
	if code != 1 {
		t.Fatalf("exit code %d", code)
	}
	os.Args = []string{"vkgen", "--help"}
	code = 0
	main()
	if code != 0 {
		t.Fatalf("--help exit code %d", code)
	}
}
