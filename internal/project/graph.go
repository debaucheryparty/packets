package project

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

type BuildGraph struct {
	components map[string]ComponentConfig
	deps       map[string][]string
	names      []string
}

func NewBuildGraph(cfg *Config) (*BuildGraph, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	g := &BuildGraph{
		components: make(map[string]ComponentConfig),
		deps:       make(map[string][]string),
		names:      make([]string, 0, len(cfg.Components)),
	}

	for _, c := range cfg.Components {
		name := c.Name
		if name == "" {
			name = c.Type
		}
		if _, exists := g.components[name]; exists {
			return nil, fmt.Errorf("duplicate component name: %q", name)
		}
		g.components[name] = c
		g.deps[name] = c.DependsOn
		g.names = append(g.names, name)
	}

	for name, deps := range g.deps {
		for _, dep := range deps {
			if _, exists := g.components[dep]; !exists {
				return nil, fmt.Errorf("component %q depends on unknown component %q", name, dep)
			}
		}
	}

	if err := g.detectCycles(); err != nil {
		return nil, err
	}

	return g, nil
}

func (g *BuildGraph) detectCycles() error {
	visited := make(map[string]int)

	var visit func(node string) error
	visit = func(node string) error {
		state := visited[node]
		if state == 1 {
			return fmt.Errorf("cycle detected in build graph involving component %q", node)
		}
		if state == 2 {
			return nil
		}
		visited[node] = 1
		for _, dep := range g.deps[node] {
			if err := visit(dep); err != nil {
				return err
			}
		}
		visited[node] = 2
		return nil
	}

	for _, name := range g.names {
		if visited[name] == 0 {
			if err := visit(name); err != nil {
				return err
			}
		}
	}
	return nil
}

func (g *BuildGraph) BuildOrder() ([]ComponentConfig, error) {
	inDegree := make(map[string]int)
	for _, name := range g.names {
		inDegree[name] = 0
	}

	dependents := make(map[string][]string)
	for name, deps := range g.deps {
		for _, dep := range deps {
			dependents[dep] = append(dependents[dep], name)
			inDegree[name]++
		}
	}

	var queue []string
	for _, name := range g.names {
		if inDegree[name] == 0 {
			queue = append(queue, name)
		}
	}

	var ordered []ComponentConfig
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		ordered = append(ordered, g.components[curr])

		for _, next := range dependents[curr] {
			inDegree[next]--
			if inDegree[next] == 0 {
				queue = append(queue, next)
			}
		}
	}

	if len(ordered) != len(g.names) {
		return nil, fmt.Errorf("failed to determine build order: unresolved cycle")
	}

	return ordered, nil
}

func (g *BuildGraph) BuildOrderFor(targetName string) ([]ComponentConfig, error) {
	if _, ok := g.components[targetName]; !ok {
		return nil, fmt.Errorf("target component %q not found", targetName)
	}

	needed := make(map[string]bool)
	var collect func(name string)
	collect = func(name string) {
		if needed[name] {
			return
		}
		needed[name] = true
		for _, dep := range g.deps[name] {
			collect(dep)
		}
	}
	collect(targetName)

	fullOrder, err := g.BuildOrder()
	if err != nil {
		return nil, err
	}

	var subset []ComponentConfig
	for _, c := range fullOrder {
		name := c.Name
		if name == "" {
			name = c.Type
		}
		if needed[name] {
			subset = append(subset, c)
		}
	}

	return subset, nil
}

type ArtifactRecorder interface {
	RecordArtifact(ctx context.Context, workflowID, stageID, path string) error
}

type NodeRunner func(ctx context.Context, c ComponentConfig) error

func (g *BuildGraph) RouteArtifacts(root string, c ComponentConfig) error {
	return g.RouteArtifactsWithJournal(context.Background(), root, c, "", "", nil)
}

func (g *BuildGraph) RouteArtifactsWithJournal(ctx context.Context, root string, c ComponentConfig, workflowID, stageID string, recorder ArtifactRecorder) error {
	if len(c.ArtifactRouting) == 0 {
		return nil
	}

	basePath := filepath.Join(root, c.Path)
	for srcRel, dstRel := range c.ArtifactRouting {
		src := filepath.Clean(filepath.Join(basePath, srcRel))
		dst := filepath.Clean(filepath.Join(root, dstRel))

		if recorder != nil && workflowID != "" {
			if err := recorder.RecordArtifact(ctx, workflowID, stageID, dst); err != nil {
				return fmt.Errorf("record artifact %s: %w", dst, err)
			}
		}

		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return fmt.Errorf("mkdir dst dir: %w", err)
		}

		if err := copyFile(src, dst); err != nil {
			return fmt.Errorf("copy artifact %s -> %s: %w", src, dst, err)
		}
	}
	return nil
}

func (g *BuildGraph) BuildDAG(ctx context.Context, concurrency int, runner NodeRunner) error {
	if len(g.names) == 0 {
		return nil
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	inDegree := make(map[string]int)
	for _, name := range g.names {
		inDegree[name] = 0
	}
	dependents := make(map[string][]string)
	for name, deps := range g.deps {
		for _, dep := range deps {
			dependents[dep] = append(dependents[dep], name)
			inDegree[name]++
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var firstErr error
	var wg sync.WaitGroup

	sem := make(chan struct{}, concurrency)
	total := len(g.names)
	completed := 0

	ready := make(chan string, total)
	for _, name := range g.names {
		if inDegree[name] == 0 {
			ready <- name
		}
	}

	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case name, ok := <-ready:
				if !ok {
					return
				}
				mu.Lock()
				if firstErr != nil {
					mu.Unlock()
					return
				}
				mu.Unlock()

				sem <- struct{}{}
				wg.Add(1)

				go func(compName string) {
					defer func() {
						<-sem
						wg.Done()
					}()

					comp := g.components[compName]
					err := runner(ctx, comp)

					mu.Lock()
					defer mu.Unlock()

					if err != nil {
						if firstErr == nil {
							firstErr = fmt.Errorf("component %q failed: %w", compName, err)
							cancel()
						}
						return
					}

					completed++
					if completed == total {
						close(done)
						return
					}

					for _, next := range dependents[compName] {
						inDegree[next]--
						if inDegree[next] == 0 {
							select {
							case ready <- next:
							case <-ctx.Done():
								return
							}
						}
					}
				}(name)
			}
		}
	}()

	select {
	case <-done:
		wg.Wait()
		return nil
	case <-ctx.Done():
		wg.Wait()
		mu.Lock()
		defer mu.Unlock()
		if firstErr != nil {
			return firstErr
		}
		return ctx.Err()
	}
}

func copyFile(src, dst string) error {
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
