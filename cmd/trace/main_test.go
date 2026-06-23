//go:build darwin

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenTraceSink_FreshPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.log")
	f, err := openTraceSink(path)
	if err != nil {
		t.Fatalf("expected file, got error: %v", err)
	}
	defer f.Close()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("file not created on disk: %v", err)
	}
}

func TestOpenTraceSink_ExistingPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "trace.log")
	if err := os.WriteFile(path, []byte("existing"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := openTraceSink(path)
	if err == nil {
		t.Fatal("expected O_EXCL error for pre-existing file, got nil")
	}
}

func TestOpenTraceSink_Dash(t *testing.T) {
	f, err := openTraceSink("-")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f != os.Stderr {
		t.Fatalf("expected os.Stderr, got %v", f)
	}
}

func TestOpenTraceSink_EmptyPath(t *testing.T) {
	dir := t.TempDir()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(orig) })

	f, err := openTraceSink("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer f.Close()
	if _, err := os.Stat(filepath.Join(dir, "leash-trace.log")); err != nil {
		t.Fatalf("expected ./leash-trace.log, stat failed: %v", err)
	}
}
