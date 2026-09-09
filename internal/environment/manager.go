package environment

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type Manager struct {
	detector *Detector
	android  *AndroidResolver
	zephyr   *ZephyrResolver
	generic  *GenericResolver
}

func NewManager() *Manager {
	return &Manager{
		detector: NewDetector(),
		android:  NewAndroidResolver(),
		zephyr:   NewZephyrResolver(),
		generic:  NewGenericResolver(),
	}
}

func (m *Manager) Detect(dir string) (*ProjectTopology, error) {
	return m.detector.DetectTopology(dir)
}

func (m *Manager) Check(ctx context.Context, dir string) (*EnvironmentReport, error) {
	topo, err := m.Detect(dir)
	if err != nil {
		return nil, fmt.Errorf("environment check detect: %w", err)
	}

	report, err := m.CheckComponents(ctx, topo.Components)
	if err != nil {
		return nil, err
	}
	report.ProjectRoot = topo.RootPath
	return report, nil
}

func (m *Manager) CheckComponents(ctx context.Context, comps []Component) (*EnvironmentReport, error) {
	report := &EnvironmentReport{
		Components: make(map[ComponentType][]ToolchainRequirement),
		AllReady:   true,
	}

	for _, comp := range comps {
		var reqs []ToolchainRequirement

		switch comp.Type {
		case ComponentAndroid:
			r, err := m.android.Check(ctx, comp, false)
			if err == nil {
				reqs = r
			}
		case ComponentZephyr:
			r, err := m.zephyr.Check(ctx, comp, false)
			if err == nil {
				reqs = r
			}
		default:
			reqs = m.generic.Check(ctx, comp)
		}

		for _, req := range reqs {
			if req.Status == StatusMissing {
				report.AllReady = false
			}
		}

		report.Components[comp.Type] = append(report.Components[comp.Type], reqs...)
	}

	return report, nil
}

func (m *Manager) Prepare(ctx context.Context, dir string, onProgress func(string)) error {
	report, err := m.Check(ctx, dir)
	if err != nil {
		return err
	}
	return m.PrepareReport(ctx, report, onProgress)
}

func (m *Manager) PrepareComponents(ctx context.Context, comps []Component, onProgress func(string)) error {
	report, err := m.CheckComponents(ctx, comps)
	if err != nil {
		return err
	}
	return m.PrepareReport(ctx, report, onProgress)
}

func (m *Manager) PrepareReport(ctx context.Context, report *EnvironmentReport, onProgress func(string)) error {
	if report.AllReady {
		if onProgress != nil {
			onProgress("Environment is already complete and ready.")
		}
		return nil
	}

	for compType, reqs := range report.Components {
		for _, req := range reqs {
			if req.Status == StatusMissing && req.CanPrepare && req.PrepareCmd != "" {
				if onProgress != nil {
					onProgress(fmt.Sprintf("Provisioning %s (%s): %s", req.Name, compType, req.PrepareCmd))
				}

				var cmd *exec.Cmd
				if runtime.GOOS == "windows" {
					cmd = exec.CommandContext(ctx, "cmd.exe", "/c", req.PrepareCmd)
				} else {
					cmd = exec.CommandContext(ctx, "sh", "-c", req.PrepareCmd)
				}

				out, err := cmd.CombinedOutput()
				if err != nil {
					return fmt.Errorf("prepare %s failed: %w\n%s", req.Name, err, strings.TrimSpace(string(out)))
				}
			}
		}
	}

	if onProgress != nil {
		onProgress("Environment preparation complete.")
	}
	return nil
}
