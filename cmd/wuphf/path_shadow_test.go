package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func writeExec(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestDetectPathShadowsFindsOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX exec-bit semantics")
	}
	root := t.TempDir()
	selfDir := filepath.Join(root, "self")
	otherDir := filepath.Join(root, "other")
	if err := os.MkdirAll(selfDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatal(err)
	}

	self := filepath.Join(selfDir, "wuphf")
	other := filepath.Join(otherDir, "wuphf")
	writeExec(t, self)
	writeExec(t, other)

	pathEnv := strings.Join([]string{selfDir, otherDir}, string(os.PathListSeparator))
	got := detectPathShadows(self, pathEnv)
	if len(got) != 1 || got[0] != other {
		t.Fatalf("want [%s], got %v", other, got)
	}
}

func TestDetectPathShadowsIgnoresSelfSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX symlink semantics")
	}
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	linkDir := filepath.Join(root, "link")
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(realDir, "wuphf")
	writeExec(t, real)
	link := filepath.Join(linkDir, "wuphf")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}

	pathEnv := strings.Join([]string{linkDir, realDir}, string(os.PathListSeparator))
	got := detectPathShadows(real, pathEnv)
	if len(got) != 0 {
		t.Fatalf("symlink to self should not shadow; got %v", got)
	}
}

func TestDetectPathShadowsSkipsDirsAndNonExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX exec-bit semantics")
	}
	root := t.TempDir()
	selfDir := filepath.Join(root, "self")
	dirDir := filepath.Join(root, "dir")
	nonExecDir := filepath.Join(root, "nonexec")
	for _, d := range []string{selfDir, dirDir, nonExecDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	self := filepath.Join(selfDir, "wuphf")
	writeExec(t, self)
	// A directory literally named "wuphf".
	if err := os.MkdirAll(filepath.Join(dirDir, "wuphf"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A non-executable regular file named "wuphf".
	nonExec := filepath.Join(nonExecDir, "wuphf")
	if err := os.WriteFile(nonExec, []byte("text"), 0o644); err != nil {
		t.Fatal(err)
	}

	pathEnv := strings.Join([]string{dirDir, nonExecDir, selfDir}, string(os.PathListSeparator))
	got := detectPathShadows(self, pathEnv)
	if len(got) != 0 {
		t.Fatalf("should skip dirs and non-exec files; got %v", got)
	}
}

func TestDetectPathShadowsEmptyPATH(t *testing.T) {
	got := detectPathShadows("/some/path/wuphf", "")
	if got != nil {
		t.Fatalf("empty PATH should yield nil; got %v", got)
	}
}

func TestDetectPathShadowsEmptySelf(t *testing.T) {
	got := detectPathShadows("", "/usr/bin")
	if got != nil {
		t.Fatalf("empty self should yield nil; got %v", got)
	}
}

func TestWarnPathShadowNoopWhenNoShadows(t *testing.T) {
	// Point self at this test binary, use an empty PATH so nothing resolves.
	self, err := os.Executable()
	if err != nil {
		t.Skip("no executable")
	}
	_ = self
	oldPath := os.Getenv("PATH")
	t.Cleanup(func() { _ = os.Setenv("PATH", oldPath) })
	_ = os.Setenv("PATH", "")
	var buf bytes.Buffer
	warnPathShadow(&buf)
	if buf.Len() != 0 {
		t.Fatalf("expected no output, got %q", buf.String())
	}
}

func TestShouldWarnShadow(t *testing.T) {
	cases := []struct {
		name                                        string
		showVersion, channelView, cmdFlagSet, piped bool
		subcmd                                      string
		want                                        bool
	}{
		{name: "interactive default", want: true},
		{name: "interactive init subcommand", subcmd: "init", want: true},
		{name: "version flag", showVersion: true, want: false},
		{name: "channel view subprocess", channelView: true, want: false},
		{name: "cmd flag scripted", cmdFlagSet: true, want: false},
		{name: "piped stdin scripted", piped: true, want: false},
		{name: "mcp-team stdio", subcmd: "mcp-team", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldWarnShadow(tc.showVersion, tc.channelView, tc.cmdFlagSet, tc.piped, tc.subcmd)
			if got != tc.want {
				t.Fatalf("want %v got %v", tc.want, got)
			}
		})
	}
}
