package apitypes

import (
	"time"
)

type ServiceStatus string

const (
	ServiceStarting ServiceStatus = "starting"
	ServiceRunning  ServiceStatus = "running"
	ServiceStopped  ServiceStatus = "stopped"
	ServiceFailed   ServiceStatus = "failed"
)

type Service struct {
	ID          string            `json:"id"`
	SubspaceID  string            `json:"subspace_id"`
	Name        string            `json:"name"`
	Image       string            `json:"image"`
	Command     []string          `json:"command"`
	Environment map[string]string `json:"environment"`
	Ports       []string          `json:"ports"`
	Status      ServiceStatus     `json:"status"`
	Driver      string            `json:"driver"`
	ContainerID string            `json:"container_id"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
}
