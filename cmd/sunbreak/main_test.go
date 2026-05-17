package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
)

func TestCLIDescriptionIsMachineReadable(t *testing.T) {
	description := cliDescription()
	if description["name"] != "sunbreak" {
		t.Fatalf("unexpected CLI name: %+v", description["name"])
	}
	flags, ok := description["flags"].([]map[string]any)
	if !ok || len(flags) == 0 {
		t.Fatalf("expected flag schema, got %+v", description["flags"])
	}
	var sawOutput bool
	for _, flag := range flags {
		if flag["name"] == "output" {
			sawOutput = true
			if flag["type"] != "enum" {
				t.Fatalf("expected output to be enum, got %+v", flag)
			}
		}
	}
	if !sawOutput {
		t.Fatal("expected output flag in schema")
	}
}

func TestWriteJSON(t *testing.T) {
	var buf bytes.Buffer
	writeJSON(&buf, map[string]any{"ok": true, "name": "sunbreak"})

	var decoded map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["ok"] != true || decoded["name"] != "sunbreak" {
		t.Fatalf("unexpected JSON payload: %+v", decoded)
	}
}

func TestWriteCommandResultJSON(t *testing.T) {
	var stdout bytes.Buffer
	oldStdout := io.Writer(&stdout)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	writeCommandResultTo(oldStdout, "json", logger, map[string]any{"ok": true, "command": "migrate"})

	var decoded map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["command"] != "migrate" {
		t.Fatalf("unexpected command result: %+v", decoded)
	}
}
