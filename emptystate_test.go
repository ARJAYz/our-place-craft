package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestStateEmptyInstancesIsArray(t *testing.T) {
	a := &App{store: &Store{Root: t.TempDir()}, prog: &Progress{}}
	b, _ := json.Marshal(a.State())
	if !strings.Contains(string(b), `"instances":[]`) {
		t.Fatal(string(b))
	}
}
