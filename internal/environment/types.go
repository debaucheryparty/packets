package environment

type ComponentType string

const (
	ComponentAndroid ComponentType = "android"
	ComponentZephyr  ComponentType = "zephyr"
	ComponentRust    ComponentType = "rust"
	ComponentGo      ComponentType = "go"
	ComponentNode    ComponentType = "node"
	ComponentPython  ComponentType = "python"
	ComponentCMake   ComponentType = "cmake"
	ComponentJava    ComponentType = "java"
	ComponentSwift   ComponentType = "swift"
	ComponentRuby    ComponentType = "ruby"
	ComponentPHP     ComponentType = "php"
	ComponentZig     ComponentType = "zig"
	ComponentDotNet  ComponentType = "dotnet"
	ComponentFlutter ComponentType = "flutter"
	ComponentDart    ComponentType = "dart"
	ComponentElixir  ComponentType = "elixir"
	ComponentGeneric ComponentType = "generic"
)

type Component struct {
	Type         ComponentType          `json:"type"`
	Name         string                 `json:"name"`
	Path         string                 `json:"path"`
	Confidence   string                 `json:"confidence"`
	Metadata     map[string]string      `json:"metadata,omitempty"`
	Requirements []ToolchainRequirement `json:"requirements,omitempty"`
}

type CheckStatus string

const (
	StatusOk      CheckStatus = "OK"
	StatusMissing CheckStatus = "MISSING"
	StatusWarning CheckStatus = "WARNING"
	StatusWrapper CheckStatus = "WRAPPER"
)

type ToolchainRequirement struct {
	Name       string      `json:"name"`
	Version    string      `json:"version,omitempty"`
	Status     CheckStatus `json:"status"`
	Details    string      `json:"details,omitempty"`
	CanPrepare bool        `json:"can_prepare"`
	PrepareCmd string      `json:"prepare_cmd,omitempty"`
}

type ProjectTopology struct {
	RootPath   string      `json:"root_path"`
	Components []Component `json:"components"`
	IsHybrid   bool        `json:"is_hybrid"`
}

type EnvironmentReport struct {
	ProjectRoot string                                   `json:"project_root"`
	Components  map[ComponentType][]ToolchainRequirement `json:"components"`
	AllReady    bool                                     `json:"all_ready"`
}

type EnvironmentID string

type EnvironmentSpec struct {
	Components []string          `json:"components"`
	Toolchains []string          `json:"toolchains"`
	Target     string            `json:"target,omitempty"`
	Versions   map[string]string `json:"versions,omitempty"`
}
