package apitypes

import "time"

type JobID string

type Job struct {
	ID          JobID
	Toolchain   Toolchain
	CacheKey    string
	State       JobState
	Provider    ProviderName
	SubmittedAt time.Time
	CompletedAt *time.Time
	ArtifactRef ArtifactRef
}

type JobState int

const (
	JobStatePending JobState = iota
	JobStateDispatched
	JobStateRunning
	JobStateSucceeded
	JobStateFailed
	JobStateFallbackLocal
)

func (s JobState) String() string {
	switch s {
	case JobStatePending:
		return "pending"
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
	Name         Toolchain
	DisplayName  string
	DetectFiles  []string
	DetectDirs   []string
	Backend      Backend
	LocalCommand string
	DefaultArgs  []string
	DockerImage  string
}

type BuildRequest struct {
	JobID       JobID
	Directory   string
	Toolchain   Toolchain
	Args        []string
	DockerImage string
}

type BuildInputs struct {
	Toolchain       Toolchain
	FileHashes      map[string]string
	CompilerVersion string
	EnvVars         map[string]string
}

type ArtifactRef string
