package main

import (
	"bytes"
	"go/token"
	"os"
	"testing"
)

func TestInvalidRoots(t *testing.T) {
	invalids := []string{
		"github.com/orijtech/consensuswarn/testdata.MissingFunc",
		"github.com/orijtech/consensuswarn/testdata.T.MissingMethod",
	}
	for _, root := range invalids {
		if _, err := runCheck(new(token.FileSet), "", bytes.NewReader(nil), []string{root}); err == nil {
			t.Errorf("root %q was unexpectedly accepted", root)
		}
	}
}

func TestPatch(t *testing.T) {
	patch, err := os.ReadFile("testdata/state1.patch")
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	roots := []string{
		"github.com/orijtech/consensuswarn/testdata.RootFunc1",
		"github.com/orijtech/consensuswarn/testdata.T.RootMethod1",
	}
	fset := new(token.FileSet)
	hunks, err := runCheck(fset, cwd, bytes.NewReader(patch), roots)
	if err != nil {
		t.Fatal(err)
	}
	if len(hunks) != 2 {
		t.Errorf("expected 2 state changing hunk, got %d", len(hunks))
	}
}
