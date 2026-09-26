package main

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func TestPacketsdHelpOutput(t *testing.T) {
	oldStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdout = w

	printUsage()

	_ = w.Close()
	os.Stdout = oldStdout

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	output := buf.String()

	if !strings.Contains(output, "Usage:") {
		t.Errorf("expected usage header in output, got: %s", output)
	}
	if !strings.Contains(output, "--help") {
		t.Errorf("expected --help flag in output, got: %s", output)
	}
	if !strings.Contains(output, "--auto-approve") {
		t.Errorf("expected --auto-approve flag in output, got: %s", output)
	}
}
