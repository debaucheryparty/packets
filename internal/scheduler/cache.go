package scheduler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type CacheKeyBuilder struct{}

func NewCacheKeyBuilder() *CacheKeyBuilder {
	return &CacheKeyBuilder{}
}

func (b *CacheKeyBuilder) Build(ctx context.Context, inputs apitypes.BuildInputs) (string, error) {
	if len(inputs.FileHashes) == 0 {
		return "", fmt.Errorf("BuildCacheKey: no file hashes provided for %s", inputs.Toolchain)
	}

	h := sha256.New()

	// v1 cache key format byte, changing this busts all existing caches
	h.Write([]byte{0x01})

	h.Write([]byte(inputs.Toolchain))
	h.Write([]byte(inputs.CompilerVersion))

	var keys []string
	for k := range inputs.FileHashes {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		h.Write([]byte(k))
		h.Write([]byte(inputs.FileHashes[k]))
	}

	var envKeys []string
	for k := range inputs.EnvVars {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)

	for _, k := range envKeys {
		h.Write([]byte(k))
		h.Write([]byte(inputs.EnvVars[k]))
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}
