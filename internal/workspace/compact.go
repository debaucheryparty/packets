package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/debaucheryparty/packets/internal/storage"
	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type Compactor struct {
	store storage.ObjectStore
}

func NewCompactor(store storage.ObjectStore) *Compactor {
	return &Compactor{store: store}
}

func (c *Compactor) Compact(ctx context.Context, owner string, keepDuration time.Duration) error {
	if c.store == nil {
		return nil
	}

	manifestPrefix := fmt.Sprintf("%s/manifests/", owner)
	manifestKeys, err := c.store.List(ctx, manifestPrefix)
	if err != nil {
		return fmt.Errorf("compact list manifests: %w", err)
	}

	if len(manifestKeys) == 0 {
		return nil
	}

	activeHashes := make(map[string]bool)
	for _, mKey := range manifestKeys {
		r, err := c.store.Download(ctx, mKey)
		if err != nil {
			continue
		}
		var manifest apitypes.WorkspaceManifest
		err = json.NewDecoder(r).Decode(&manifest)
		r.Close() //nolint:errcheck
		if err != nil {
			continue
		}
		for _, f := range manifest.Files {
			if f.Hash != "" {
				activeHashes[f.Hash] = true
			}
		}
	}

	chunkPrefix := fmt.Sprintf("%s/chunks/", owner)
	chunkKeys, err := c.store.List(ctx, chunkPrefix)
	if err != nil {
		return fmt.Errorf("compact list chunks: %w", err)
	}

	for _, cKey := range chunkKeys {
		hash := cKey[len(chunkPrefix):]
		if !activeHashes[hash] {
			_ = c.store.Delete(ctx, cKey)
		}
	}

	return nil
}

func readAllFrom(r io.Reader) ([]byte, error) { //nolint:unused
	return io.ReadAll(r)
}
