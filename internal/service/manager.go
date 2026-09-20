package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

var (
	ErrInvalidSubspaceID  = errors.New("subspace ID is required")
	ErrInvalidServiceName = errors.New("service name is required")
	ErrServiceNotFound    = errors.New("service not found")
)

type Store interface {
	CreateService(ctx context.Context, svc apitypes.Service) error
	GetService(ctx context.Context, subspaceID, nameOrID string) (apitypes.Service, error)
	ListServices(ctx context.Context, subspaceID string) ([]apitypes.Service, error)
	UpdateServiceStatus(ctx context.Context, id string, status apitypes.ServiceStatus, containerID string) error
	DeleteService(ctx context.Context, subspaceID, nameOrID string) error
}

type Manager struct {
	store   Store
	drivers map[string]Driver
	logger  *slog.Logger
	mu      sync.RWMutex
}

func NewManager(store Store, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	m := &Manager{
		store:   store,
		drivers: make(map[string]Driver),
		logger:  logger,
	}
	m.RegisterDriver("process", NewProcessDriver())
	return m
}

func (m *Manager) RegisterDriver(name string, driver Driver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.drivers[name] = driver
}

func (m *Manager) getDriver(name string) (Driver, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if name == "" {
		name = "process"
	}
	d, ok := m.drivers[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrDriverNotFound, name)
	}
	return d, nil
}

func (m *Manager) Start(ctx context.Context, subspaceID string, svc apitypes.Service) (apitypes.Service, error) {
	if subspaceID == "" {
		return apitypes.Service{}, ErrInvalidSubspaceID
	}
	if svc.Name == "" {
		return apitypes.Service{}, ErrInvalidServiceName
	}
	if svc.Driver == "" {
		svc.Driver = "process"
	}

	driver, err := m.getDriver(svc.Driver)
	if err != nil {
		return apitypes.Service{}, err
	}

	existing, err := m.store.GetService(ctx, subspaceID, svc.Name)
	if err == nil {
		if existing.Status == apitypes.ServiceRunning {
			return existing, nil
		}
		containerID, startErr := driver.Start(ctx, existing)
		if startErr != nil {
			_ = m.store.UpdateServiceStatus(ctx, existing.ID, apitypes.ServiceFailed, "")
			return apitypes.Service{}, fmt.Errorf("start existing service: %w", startErr)
		}
		if updateErr := m.store.UpdateServiceStatus(ctx, existing.ID, apitypes.ServiceRunning, containerID); updateErr != nil {
			return apitypes.Service{}, fmt.Errorf("update service status: %w", updateErr)
		}
		existing.Status = apitypes.ServiceRunning
		existing.ContainerID = containerID
		return existing, nil
	}

	now := time.Now().UTC()
	svc.ID = generateServiceID()
	svc.SubspaceID = subspaceID
	svc.Status = apitypes.ServiceStarting
	svc.CreatedAt = now
	svc.UpdatedAt = now

	if err := m.store.CreateService(ctx, svc); err != nil {
		return apitypes.Service{}, fmt.Errorf("create service record: %w", err)
	}

	containerID, err := driver.Start(ctx, svc)
	if err != nil {
		_ = m.store.UpdateServiceStatus(ctx, svc.ID, apitypes.ServiceFailed, "")
		return apitypes.Service{}, fmt.Errorf("start service process: %w", err)
	}

	if err := m.store.UpdateServiceStatus(ctx, svc.ID, apitypes.ServiceRunning, containerID); err != nil {
		return apitypes.Service{}, fmt.Errorf("update service running status: %w", err)
	}

	svc.Status = apitypes.ServiceRunning
	svc.ContainerID = containerID
	m.logger.InfoContext(ctx, "service started",
		slog.String("id", svc.ID),
		slog.String("name", svc.Name),
		slog.String("subspace_id", svc.SubspaceID),
		slog.String("driver", svc.Driver),
	)

	return svc, nil
}

func (m *Manager) Stop(ctx context.Context, subspaceID, nameOrID string) (apitypes.Service, error) {
	if subspaceID == "" {
		return apitypes.Service{}, ErrInvalidSubspaceID
	}
	svc, err := m.store.GetService(ctx, subspaceID, nameOrID)
	if err != nil {
		return apitypes.Service{}, err
	}

	if svc.Status == apitypes.ServiceStopped {
		return svc, nil
	}

	driver, err := m.getDriver(svc.Driver)
	if err == nil && svc.ContainerID != "" {
		_ = driver.Stop(ctx, svc.ContainerID)
	}

	if err := m.store.UpdateServiceStatus(ctx, svc.ID, apitypes.ServiceStopped, ""); err != nil {
		return apitypes.Service{}, fmt.Errorf("update service stopped status: %w", err)
	}

	svc.Status = apitypes.ServiceStopped
	svc.ContainerID = ""
	m.logger.InfoContext(ctx, "service stopped",
		slog.String("id", svc.ID),
		slog.String("name", svc.Name),
		slog.String("subspace_id", svc.SubspaceID),
	)

	return svc, nil
}

func (m *Manager) Restart(ctx context.Context, subspaceID, nameOrID string) (apitypes.Service, error) {
	svc, err := m.Stop(ctx, subspaceID, nameOrID)
	if err != nil {
		return apitypes.Service{}, err
	}

	driver, err := m.getDriver(svc.Driver)
	if err != nil {
		return apitypes.Service{}, err
	}

	containerID, err := driver.Start(ctx, svc)
	if err != nil {
		_ = m.store.UpdateServiceStatus(ctx, svc.ID, apitypes.ServiceFailed, "")
		return apitypes.Service{}, fmt.Errorf("restart service process: %w", err)
	}

	if err := m.store.UpdateServiceStatus(ctx, svc.ID, apitypes.ServiceRunning, containerID); err != nil {
		return apitypes.Service{}, fmt.Errorf("update service restarted status: %w", err)
	}

	svc.Status = apitypes.ServiceRunning
	svc.ContainerID = containerID
	m.logger.InfoContext(ctx, "service restarted",
		slog.String("id", svc.ID),
		slog.String("name", svc.Name),
		slog.String("subspace_id", svc.SubspaceID),
	)

	return svc, nil
}

func (m *Manager) List(ctx context.Context, subspaceID string) ([]apitypes.Service, error) {
	if subspaceID == "" {
		return nil, ErrInvalidSubspaceID
	}
	return m.store.ListServices(ctx, subspaceID)
}

func (m *Manager) Logs(ctx context.Context, subspaceID, nameOrID string, lines int) (string, error) {
	if subspaceID == "" {
		return "", ErrInvalidSubspaceID
	}
	svc, err := m.store.GetService(ctx, subspaceID, nameOrID)
	if err != nil {
		return "", err
	}

	if svc.ContainerID == "" {
		return fmt.Sprintf("Service %s is not running\n", svc.Name), nil
	}

	driver, err := m.getDriver(svc.Driver)
	if err != nil {
		return "", err
	}

	return driver.Logs(ctx, svc.ContainerID, lines)
}

func (m *Manager) Delete(ctx context.Context, subspaceID, nameOrID string) error {
	_, _ = m.Stop(ctx, subspaceID, nameOrID)
	return m.store.DeleteService(ctx, subspaceID, nameOrID)
}

func generateServiceID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "svc-" + hex.EncodeToString(b)
}
