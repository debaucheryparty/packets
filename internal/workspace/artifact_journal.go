package workspace

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type JournalAction string

const (
	ActionCreated     JournalAction = "CREATED"
	ActionOverwritten JournalAction = "OVERWRITTEN"
)

type JournalEntry struct {
	WorkflowID string        `json:"workflow_id"`
	StageID    string        `json:"stage_id"`
	Path       string        `json:"path"`
	Action     JournalAction `json:"action"`
	BackupPath string        `json:"backup_path,omitempty"`
}

type ArtifactJournal struct {
	mu         sync.Mutex
	journalDir string
}

func NewArtifactJournal(baseDir string) (*ArtifactJournal, error) {
	jDir := filepath.Join(baseDir, ".packets", "journal")
	if err := os.MkdirAll(jDir, 0o755); err != nil {
		return nil, fmt.Errorf("create journal dir: %w", err)
	}
	return &ArtifactJournal{
		journalDir: jDir,
	}, nil
}

func (aj *ArtifactJournal) workflowFile(workflowID string) string {
	return filepath.Join(aj.journalDir, fmt.Sprintf("wf_%s.json", workflowID))
}

func (aj *ArtifactJournal) backupDir(workflowID string) string {
	return filepath.Join(aj.journalDir, "backups", workflowID)
}

func (aj *ArtifactJournal) loadEntries(workflowID string) ([]JournalEntry, error) {
	pFile := aj.workflowFile(workflowID)
	data, err := os.ReadFile(pFile)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var entries []JournalEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

func (aj *ArtifactJournal) saveEntries(workflowID string, entries []JournalEntry) error {
	pFile := aj.workflowFile(workflowID)
	if len(entries) == 0 {
		_ = os.Remove(pFile)
		_ = os.RemoveAll(aj.backupDir(workflowID))
		return nil
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pFile, data, 0o644)
}

func (aj *ArtifactJournal) RecordArtifact(ctx context.Context, workflowID, stageID, targetPath string) error {
	aj.mu.Lock()
	defer aj.mu.Unlock()

	cleanedPath := filepath.Clean(targetPath)
	entries, err := aj.loadEntries(workflowID)
	if err != nil {
		return fmt.Errorf("load journal: %w", err)
	}

	for _, e := range entries {
		if e.Path == cleanedPath {
			return nil
		}
	}

	entry := JournalEntry{
		WorkflowID: workflowID,
		StageID:    stageID,
		Path:       cleanedPath,
	}

	if fi, err := os.Stat(cleanedPath); err == nil && !fi.IsDir() {
		bDir := aj.backupDir(workflowID)
		if err := os.MkdirAll(bDir, 0o755); err != nil {
			return fmt.Errorf("mkdir backup dir: %w", err)
		}
		backupFile := filepath.Join(bDir, fmt.Sprintf("bak_%d_%s", len(entries), filepath.Base(cleanedPath)))
		if err := copyFileInternal(cleanedPath, backupFile); err != nil {
			return fmt.Errorf("backup existing file: %w", err)
		}
		entry.Action = ActionOverwritten
		entry.BackupPath = backupFile
	} else {
		entry.Action = ActionCreated
	}

	entries = append(entries, entry)
	return aj.saveEntries(workflowID, entries)
}

func (aj *ArtifactJournal) Compensate(ctx context.Context, workflowID string) error {
	aj.mu.Lock()
	defer aj.mu.Unlock()

	entries, err := aj.loadEntries(workflowID)
	if err != nil {
		return fmt.Errorf("load journal for compensation: %w", err)
	}
	if len(entries) == 0 {
		return nil
	}

	var compensationErrors []error

	for i := len(entries) - 1; i >= 0; i-- {
		entry := entries[i]
		switch entry.Action {
		case ActionCreated:
			if _, err := os.Stat(entry.Path); err == nil {
				if err := os.Remove(entry.Path); err != nil && !os.IsNotExist(err) {
					compensationErrors = append(compensationErrors, fmt.Errorf("remove created file %s: %w", entry.Path, err))
				}
			}
		case ActionOverwritten:
			if entry.BackupPath != "" {
				if _, err := os.Stat(entry.BackupPath); err == nil {
					if err := copyFileInternal(entry.BackupPath, entry.Path); err != nil {
						compensationErrors = append(compensationErrors, fmt.Errorf("restore backup %s -> %s: %w", entry.BackupPath, entry.Path, err))
					}
				}
			}
		}
	}

	_ = aj.saveEntries(workflowID, nil)

	if len(compensationErrors) > 0 {
		return fmt.Errorf("compensation completed with errors: %v", compensationErrors)
	}
	return nil
}

func (aj *ArtifactJournal) ListUncompensatedWorkflows() ([]string, error) {
	aj.mu.Lock()
	defer aj.mu.Unlock()

	files, err := os.ReadDir(aj.journalDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var wfs []string
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".json" {
			name := f.Name()
			if len(name) > 8 && name[:3] == "wf_" {
				wfID := name[3 : len(name)-5]
				wfs = append(wfs, wfID)
			}
		}
	}
	return wfs, nil
}

func copyFileInternal(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()

	_, err = io.Copy(out, in)
	return err
}
