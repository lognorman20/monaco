package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestParseSweepFlags_requiresDestination(t *testing.T) {
	t.Parallel()

	if _, err := parseSweepFlags(nil, bytes.NewBuffer(nil)); err == nil {
		t.Fatal("expected error")
	}

	_, err := parseSweepFlags([]string{"--all"}, bytes.NewBuffer(nil))
	if err == nil || !strings.Contains(err.Error(), "--destination is required") {
		t.Fatalf("expected missing destination error, got %v", err)
	}
}

func TestParseSweepFlags_oneSource(t *testing.T) {
	t.Parallel()

	flags, err := parseSweepFlags([]string{
		"--destination", "Dest111",
		"--source", "Src111",
	}, bytes.NewBuffer(nil))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if flags.destination != "Dest111" {
		t.Fatalf("destination = %q", flags.destination)
	}
	if len(flags.sources) != 1 || flags.sources[0] != "Src111" {
		t.Fatalf("sources = %#v", flags.sources)
	}
	if flags.all {
		t.Fatal("all should be false")
	}
}

func TestParseSweepFlags_manySources(t *testing.T) {
	t.Parallel()

	flags, err := parseSweepFlags([]string{
		"--destination", "Dest111",
		"--source", "Src111",
		"--source", "Src222,Src333",
		"--dry-run",
	}, bytes.NewBuffer(nil))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	want := []string{"Src111", "Src222", "Src333"}
	if len(flags.sources) != len(want) {
		t.Fatalf("sources = %#v, want %#v", flags.sources, want)
	}
	for i := range want {
		if flags.sources[i] != want[i] {
			t.Fatalf("sources[%d] = %q, want %q", i, flags.sources[i], want[i])
		}
	}
	if !flags.dryRun {
		t.Fatal("dryRun should be true")
	}
}

func TestParseSweepFlags_allConflict(t *testing.T) {
	t.Parallel()

	_, err := parseSweepFlags([]string{
		"--destination", "Dest111",
		"--all",
		"--source", "Src111",
	}, bytes.NewBuffer(nil))
	if err == nil || !strings.Contains(err.Error(), "--all cannot be combined with --source") {
		t.Fatalf("expected all/source conflict, got %v", err)
	}
}

func TestParseSweepFlags_allAndDryRun(t *testing.T) {
	t.Parallel()

	flags, err := parseSweepFlags([]string{"--destination", "Dest111", "--all", "--dry-run"}, bytes.NewBuffer(nil))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if flags.destination != "Dest111" {
		t.Fatalf("destination = %q", flags.destination)
	}
	if !flags.all || !flags.dryRun {
		t.Fatalf("all=%t dryRun=%t", flags.all, flags.dryRun)
	}
	if len(flags.sources) != 0 {
		t.Fatalf("sources = %#v", flags.sources)
	}
}
