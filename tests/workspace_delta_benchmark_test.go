package tests

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func computeSHA256(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func computeSimpleDelta(base, target []byte) []byte {
	var delta bytes.Buffer
	minLen := len(base)
	if len(target) < minLen {
		minLen = len(target)
	}

	prefixLen := 0
	for prefixLen < minLen && base[prefixLen] == target[prefixLen] {
		prefixLen++
	}

	suffixLen := 0
	for suffixLen < (minLen-prefixLen) && base[len(base)-1-suffixLen] == target[len(target)-1-suffixLen] {
		suffixLen++
	}

	targetMiddle := target[prefixLen : len(target)-suffixLen]
	delta.Write(targetMiddle)
	return delta.Bytes()
}

func BenchmarkWorkspaceTransfer_Comparison(b *testing.B) {
	baseSource := bytes.Repeat([]byte("package main\nfunc Add(a, b int) int { return a + b }\n"), 100)
	targetSource := bytes.Repeat([]byte("package main\nfunc Add(a, b int) int { return a + b }\n"), 100)
	targetSource = append(targetSource, []byte("// modified line\n")...)

	b.Run("FullChunkTransfer", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			_ = computeSHA256(targetSource)
			_ = len(targetSource)
		}
	})

	b.Run("DeltaTransfer", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			delta := computeSimpleDelta(baseSource, targetSource)
			_ = len(delta)
		}
	})
}

func TestWorkspaceDelta_CorrectnessAndSavings(t *testing.T) {
	baseText := []byte("function calculateTotal(items) {\n  let total = 0;\n  for (const item of items) {\n    total += item.price;\n  }\n  return total;\n}\n")
	modText := []byte("function calculateTotal(items) {\n  let total = 0;\n  for (const item of items) {\n    total += item.price * (1 + item.tax);\n  }\n  return total;\n}\n")

	baseHash := computeSHA256(baseText)
	modHash := computeSHA256(modText)

	if baseHash == modHash {
		t.Errorf("base and modified hashes must differ")
	}

	delta := computeSimpleDelta(baseText, modText)
	if len(delta) >= len(modText) {
		t.Errorf("delta should be smaller than full content: delta=%d, full=%d", len(delta), len(modText))
	}
}
