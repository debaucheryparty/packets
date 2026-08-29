package apitypes

import "time"

type JobState int

const (
	JobStatePending       JobState = 0
	JobStateUploading     JobState = 1
	JobStateDispatched    JobState = 2
	JobStateRunning       JobState = 3
	JobStateSucceeded     JobState = 4
	JobStateFailed        JobState = 5
	JobStateFallbackLocal JobState = 6
)

func (s JobState) String() string {
	switch s {
	case JobStatePending:
		return "pending"
	case JobStateUploading:
		return "uploading"
	case JobStateDispatched:
		return "dispatched"
	case JobStateRunning:
		return "running"
	case JobStateSucceeded:
		return "succeeded"
	case JobStateFailed:
		return "failed"
	case JobStateFallbackLocal:
		return "fallback_local"
	default:
		return "unknown"
	}
}

func (s JobState) IsTerminal() bool {
	return s == JobStateSucceeded || s == JobStateFailed || s == JobStateFallbackLocal
}

type RunnerName string

const (
	RunnerDocker   RunnerName = "docker"
	RunnerGitHub   RunnerName = "github"
	RunnerLocal    RunnerName = "local"
	RunnerCircleCI RunnerName = "circleci"
)

type RunnerDef struct {
	Name           RunnerName
	DisplayName    string
	RequiresUpload bool
	RequiresGit    bool
}

type SourceMode string

const (
	SourceModeWorkspace SourceMode = "workspace"
	SourceModeGit       SourceMode = "git"
)

type ProviderName string

const (
	ProviderGitHubActions ProviderName = "github_actions"
	ProviderCircleCI      ProviderName = "circleci"
	ProviderDockerWorker  ProviderName = "docker_worker"
)

type Toolchain string

const (
	ToolchainRust    Toolchain = "rust"
	ToolchainGo      Toolchain = "go"
	ToolchainKotlin  Toolchain = "kotlin"
	ToolchainSwift   Toolchain = "swift"
	ToolchainC       Toolchain = "c"
	ToolchainCPP     Toolchain = "cpp"
	ToolchainJava    Toolchain = "java"
	ToolchainScala   Toolchain = "scala"
	ToolchainPython  Toolchain = "python"
	ToolchainNode    Toolchain = "node"
	ToolchainRuby    Toolchain = "ruby"
	ToolchainPHP     Toolchain = "php"
	ToolchainElixir  Toolchain = "elixir"
	ToolchainHaskell Toolchain = "haskell"
	ToolchainZig     Toolchain = "zig"
	ToolchainNim     Toolchain = "nim"
	ToolchainOCaml   Toolchain = "ocaml"
	ToolchainDart    Toolchain = "dart"
	ToolchainDotNet  Toolchain = "dotnet"
	ToolchainCrystal Toolchain = "crystal"
	ToolchainClojure Toolchain = "clojure"
	ToolchainPerl    Toolchain = "perl"
	ToolchainLua     Toolchain = "lua"
	ToolchainR       Toolchain = "r"
	ToolchainJulia   Toolchain = "julia"
	ToolchainD       Toolchain = "d"
	ToolchainV       Toolchain = "v"
	ToolchainErlang  Toolchain = "erlang"
	ToolchainGroovy  Toolchain = "groovy"
	ToolchainFlutter Toolchain = "flutter"
	ToolchainObjC    Toolchain = "objc"
)

func (t Toolchain) String() string { return string(t) }

type Backend int

const (
	BackendSccacheDist Backend = iota
	BackendGradleCache
	BackendScheduler
	BackendCIProvider
	BackendLocal
)

func (b Backend) String() string {
	switch b {
	case BackendSccacheDist:
		return "sccache_dist"
	case BackendGradleCache:
		return "gradle_cache"
	case BackendScheduler:
		return "scheduler"
	case BackendCIProvider:
		return "ci_provider"
	case BackendLocal:
		return "local"
	default:
		return "unknown"
	}
}

type ToolchainDef struct {
	Name             Toolchain
	DisplayName      string
	DetectFiles      []string
	DetectDirs       []string
	Backend          Backend
	LocalCommand     string
	DefaultArgs      []string
	DockerImage      string
	DefaultArtifacts []string
}

type WorkspaceManifest struct {
	RootHash string          `json:"root_hash"`
	Files    []WorkspaceFile `json:"files"`
}

type WorkspaceFile struct {
	Path  string `json:"path"`
	Hash  string `json:"hash"`
	Size  int64  `json:"size"`
	Mode  uint32 `json:"mode"`
	IsDir bool   `json:"is_dir"`
	Link  string `json:"link,omitempty"`
}

type JobID string

type Job struct {
	ID            JobID
	Toolchain     Toolchain
	CacheKey      string
	State         JobState
	Provider      ProviderName
	Runner        RunnerName
	SourceMode    SourceMode
	SnapshotRef   string
	CommandArgs   []string
	ArtifactPaths []string
	Image         string
	Error         string
	Owner         string
	SubmittedAt   time.Time
	CompletedAt   *time.Time
	ArtifactRef   ArtifactRef
}

type BuildRequest struct {
	JobID         JobID
	Directory     string
	Toolchain     Toolchain
	Args          []string
	DockerImage   string
	Runner        RunnerName
	SourceMode    SourceMode
	SnapshotRef   string
	CommandArgs   []string
	ArtifactPaths []string
}

type BuildInputs struct {
	Toolchain       Toolchain
	FileHashes      map[string]string
	CompilerVersion string
	EnvVars         map[string]string
}

type ExecutionResult struct {
	ExitCode    int
	Stdout      string
	Stderr      string
	ArtifactRef ArtifactRef
	Error       error
}

type ArtifactRef string
