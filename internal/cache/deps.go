// Package cache provides multi-layer dependency caching for remote builds.
//
// # Dependency Cache (Phase 10)
//
// Remote builds are expensive primarily because of dependency downloads.
// This package ensures that each toolchain's package cache (Gradle, Cargo,
// Go modules, pip, npm, west) is stored in a persistent, per-owner directory
// on the remote worker and reused across sequential builds.
//
// Usage:
//
//	mgr := cache.NewDepManager("/var/lib/packets/dep-caches")
//	env, err := mgr.Bind(ctx, owner, toolchain, workspaceDir)
//	// run build command with env appended to os.Environ()
package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

// DepManager manages persistent per-toolchain dependency caches on the remote
// worker host.  Each owner gets an isolated cache root, and within that root
// each toolchain gets its own sub-directory.
type DepManager struct {
	// cacheRoot is the host directory that holds all per-owner caches.
	// Typical value: /var/lib/packets/dep-caches  (or ~/.packets/dep-caches)
	cacheRoot string
}

// NewDepManager creates a DepManager that stores caches under cacheRoot.
// The directory is created if it does not exist.
func NewDepManager(cacheRoot string) *DepManager {
	if cacheRoot == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			cacheRoot = filepath.Join(home, ".packets", "dep-caches")
		} else {
			cacheRoot = filepath.Join(os.TempDir(), "packets-dep-caches")
		}
	}
	_ = os.MkdirAll(cacheRoot, 0o755)
	return &DepManager{cacheRoot: cacheRoot}
}

// NewDepManagerFromEnv creates a DepManager, respecting PACKETS_DEP_CACHE_ROOT
// if set, otherwise falling back to the default path.
func NewDepManagerFromEnv() *DepManager {
	return NewDepManager(os.Getenv("PACKETS_DEP_CACHE_ROOT"))
}

// cacheDir returns the per-owner per-toolchain cache directory, creating it if
// necessary.
func (m *DepManager) cacheDir(owner string, toolchain apitypes.Toolchain) string {
	safe := strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, string(toolchain))
	dir := filepath.Join(m.cacheRoot, owner, safe)
	_ = os.MkdirAll(dir, 0o755)
	return dir
}

// CacheBinding describes the environment variables and directory bind-mounts
// that redirect a toolchain's dependency cache to the persistent location.
type CacheBinding struct {
	// Env is a list of "KEY=VALUE" pairs to append to the build process's
	// environment.
	Env []string

	// BindMounts is a list of (source, dest) pairs for tools that do not
	// respect environment variable overrides.  The executor symlinks or
	// bind-mounts source→dest before running the job.
	BindMounts []BindMount
}

// BindMount describes a directory that should be made visible at a specific
// path inside the workspace or on the host.
type BindMount struct {
	// HostPath is the persistent cache directory on the worker.
	HostPath string
	// WorkspacePath is the path relative to the workspace root where the cache
	// should appear.  Empty means a host-level symlink is used instead.
	WorkspacePath string
}

// Bind prepares the persistent cache directories for a job and returns a
// CacheBinding containing the environment variables and bind-mounts the
// executor should apply before launching the build process.
//
// workspaceDir is the directory where the project source has been extracted.
func (m *DepManager) Bind(_ context.Context, owner string, toolchain apitypes.Toolchain, workspaceDir string) (*CacheBinding, error) {
	cdir := m.cacheDir(owner, toolchain)

	switch toolchain {
	// ── JVM family (Gradle-based) ────────────────────────────────────────────
	case apitypes.ToolchainAndroid, apitypes.ToolchainKotlin,
		apitypes.ToolchainJava, apitypes.ToolchainScala,
		apitypes.ToolchainGroovy:
		return m.bindGradle(cdir, workspaceDir, toolchain)

	// ── Systems / native ─────────────────────────────────────────────────────
	case apitypes.ToolchainRust:
		return m.bindRust(cdir)

	case apitypes.ToolchainGo:
		return m.bindGo(cdir)

	case apitypes.ToolchainC, apitypes.ToolchainCPP:
		return m.bindC(cdir)

	case apitypes.ToolchainZig:
		return m.bindZig(cdir)

	case apitypes.ToolchainNim:
		return m.bindNim(cdir)

	case apitypes.ToolchainD:
		return m.bindD(cdir)

	case apitypes.ToolchainV:
		return m.bindV(cdir)

	case apitypes.ToolchainCrystal:
		return m.bindCrystal(cdir)

	// ── Scripting / interpreted ───────────────────────────────────────────────
	case apitypes.ToolchainPython:
		return m.bindPython(cdir, workspaceDir)

	case apitypes.ToolchainNode:
		return m.bindNode(cdir, workspaceDir)

	case apitypes.ToolchainRuby:
		return m.bindRuby(cdir, workspaceDir)

	case apitypes.ToolchainPHP:
		return m.bindPHP(cdir, workspaceDir)

	case apitypes.ToolchainPerl:
		return m.bindPerl(cdir)

	case apitypes.ToolchainLua:
		return m.bindLua(cdir)

	case apitypes.ToolchainR:
		return m.bindR(cdir)

	case apitypes.ToolchainJulia:
		return m.bindJulia(cdir)

	// ── Functional ───────────────────────────────────────────────────────────
	case apitypes.ToolchainElixir, apitypes.ToolchainErlang:
		return m.bindElixir(cdir)

	case apitypes.ToolchainHaskell:
		return m.bindHaskell(cdir)

	case apitypes.ToolchainOCaml:
		return m.bindOCaml(cdir)

	case apitypes.ToolchainClojure:
		return m.bindClojure(cdir)

	// ── Mobile / cross-platform ───────────────────────────────────────────────
	case apitypes.ToolchainSwift, apitypes.ToolchainObjC:
		return m.bindSwift(cdir)

	case apitypes.ToolchainDart, apitypes.ToolchainFlutter:
		return m.bindDart(cdir)

	case apitypes.ToolchainDotNet:
		return m.bindDotNet(cdir)

	// ── Embedded ─────────────────────────────────────────────────────────────
	case apitypes.ToolchainZephyr:
		return m.bindZephyr(cdir, workspaceDir)

	// ── Generic / exec ───────────────────────────────────────────────────────
	default:
		return &CacheBinding{}, nil
	}
}

// ── JVM / Gradle family ──────────────────────────────────────────────────────

// bindGradle covers Android, Kotlin, Java, Scala, and Groovy — all of which
// use Gradle as their primary build tool.  Android additionally needs
// ANDROID_HOME propagated.
func (m *DepManager) bindGradle(cdir, workspaceDir string, toolchain apitypes.Toolchain) (*CacheBinding, error) {
	gradleHome := filepath.Join(cdir, "gradle-home")
	_ = os.MkdirAll(gradleHome, 0o755)

	env := []string{"GRADLE_USER_HOME=" + gradleHome}

	if toolchain == apitypes.ToolchainAndroid {
		androidHome := os.Getenv("ANDROID_HOME")
		if androidHome == "" {
			androidHome = os.Getenv("ANDROID_SDK_ROOT")
		}
		if androidHome != "" {
			env = append(env, "ANDROID_HOME="+androidHome, "ANDROID_SDK_ROOT="+androidHome)
		}
	}

	_ = workspaceDir
	return &CacheBinding{Env: env}, nil
}

// ── Systems / native ─────────────────────────────────────────────────────────

// bindRust directs Cargo to a persistent registry and target cache.
func (m *DepManager) bindRust(cdir string) (*CacheBinding, error) {
	cargoHome := filepath.Join(cdir, "cargo-home")
	_ = os.MkdirAll(filepath.Join(cargoHome, "registry"), 0o755)
	_ = os.MkdirAll(filepath.Join(cargoHome, "git"), 0o755)

	return &CacheBinding{
		Env: []string{
			"CARGO_HOME=" + cargoHome,
			"CARGO_TARGET_DIR=" + filepath.Join(cdir, "cargo-target"),
		},
	}, nil
}

// bindGo directs the Go toolchain to a persistent module and build cache.
func (m *DepManager) bindGo(cdir string) (*CacheBinding, error) {
	gopath := filepath.Join(cdir, "gopath")
	_ = os.MkdirAll(filepath.Join(gopath, "pkg", "mod"), 0o755)
	gocache := filepath.Join(cdir, "go-build-cache")
	_ = os.MkdirAll(gocache, 0o755)

	return &CacheBinding{
		Env: []string{
			"GOPATH=" + gopath,
			"GOCACHE=" + gocache,
		},
	}, nil
}

// bindC/C++ uses ccache for compiler output caching.
func (m *DepManager) bindC(cdir string) (*CacheBinding, error) {
	ccacheDir := filepath.Join(cdir, "ccache")
	_ = os.MkdirAll(ccacheDir, 0o755)

	return &CacheBinding{
		Env: []string{
			"CCACHE_DIR=" + ccacheDir,
			// Tell CMake / autotools to wrap the compiler with ccache.
			"CMAKE_C_COMPILER_LAUNCHER=ccache",
			"CMAKE_CXX_COMPILER_LAUNCHER=ccache",
		},
	}, nil
}

// bindZig stores the Zig global cache (downloaded packages, std lib builds).
func (m *DepManager) bindZig(cdir string) (*CacheBinding, error) {
	zigCache := filepath.Join(cdir, "zig-cache")
	_ = os.MkdirAll(zigCache, 0o755)
	zigGlobal := filepath.Join(cdir, "zig-global-cache")
	_ = os.MkdirAll(zigGlobal, 0o755)

	return &CacheBinding{
		Env: []string{
			"ZIG_GLOBAL_CACHE_DIR=" + zigGlobal,
			"ZIG_LOCAL_CACHE_DIR=" + zigCache,
		},
	}, nil
}

// bindNim stores the Nimble package cache.
func (m *DepManager) bindNim(cdir string) (*CacheBinding, error) {
	nimbleDir := filepath.Join(cdir, "nimble")
	_ = os.MkdirAll(nimbleDir, 0o755)

	return &CacheBinding{
		Env: []string{"NIMBLE_DIR=" + nimbleDir},
	}, nil
}

// bindD stores DUB (D package manager) package cache.
func (m *DepManager) bindD(cdir string) (*CacheBinding, error) {
	dubCache := filepath.Join(cdir, "dub")
	_ = os.MkdirAll(dubCache, 0o755)

	return &CacheBinding{
		Env: []string{"DUB_HOME=" + dubCache},
	}, nil
}

// bindV stores V module downloads.
func (m *DepManager) bindV(cdir string) (*CacheBinding, error) {
	vModules := filepath.Join(cdir, "vmodules")
	_ = os.MkdirAll(vModules, 0o755)

	return &CacheBinding{
		Env: []string{"VMODULES=" + vModules},
	}, nil
}

// bindCrystal stores Shards (Crystal package manager) cache.
func (m *DepManager) bindCrystal(cdir string) (*CacheBinding, error) {
	shardsCache := filepath.Join(cdir, "shards-cache")
	_ = os.MkdirAll(shardsCache, 0o755)

	return &CacheBinding{
		Env: []string{"SHARDS_CACHE_PATH=" + shardsCache},
	}, nil
}

// ── Scripting / interpreted ───────────────────────────────────────────────────

// bindNode symlinks node_modules into the workspace and redirects npm cache.
func (m *DepManager) bindNode(cdir, workspaceDir string) (*CacheBinding, error) {
	npmCache := filepath.Join(cdir, "npm-cache")
	_ = os.MkdirAll(npmCache, 0o755)

	nodeModulesCache := filepath.Join(cdir, "node_modules")
	_ = os.MkdirAll(nodeModulesCache, 0o755)

	var mounts []BindMount
	if workspaceDir != "" {
		mounts = append(mounts, BindMount{
			HostPath:      nodeModulesCache,
			WorkspacePath: "node_modules",
		})
	}

	return &CacheBinding{
		Env:        []string{"npm_config_cache=" + npmCache},
		BindMounts: mounts,
	}, nil
}

// bindPython directs pip to a persistent wheel cache and symlinks .venv.
func (m *DepManager) bindPython(cdir, workspaceDir string) (*CacheBinding, error) {
	pipCache := filepath.Join(cdir, "pip-cache")
	_ = os.MkdirAll(pipCache, 0o755)

	venvCache := filepath.Join(cdir, "venv")
	_ = os.MkdirAll(venvCache, 0o755)

	var mounts []BindMount
	if workspaceDir != "" {
		mounts = append(mounts, BindMount{
			HostPath:      venvCache,
			WorkspacePath: ".venv",
		})
	}

	return &CacheBinding{
		Env: []string{
			"PIP_CACHE_DIR=" + pipCache,
			"VIRTUALENVS_PATH=" + venvCache,
		},
		BindMounts: mounts,
	}, nil
}

// bindRuby caches gems in a persistent vendor/bundle directory.
func (m *DepManager) bindRuby(cdir, workspaceDir string) (*CacheBinding, error) {
	gemHome := filepath.Join(cdir, "gem-home")
	_ = os.MkdirAll(gemHome, 0o755)
	bundlePath := filepath.Join(cdir, "bundle")
	_ = os.MkdirAll(bundlePath, 0o755)

	var mounts []BindMount
	if workspaceDir != "" {
		mounts = append(mounts, BindMount{
			HostPath:      bundlePath,
			WorkspacePath: "vendor/bundle",
		})
	}

	return &CacheBinding{
		Env: []string{
			"GEM_HOME=" + gemHome,
			"BUNDLE_PATH=" + bundlePath,
		},
		BindMounts: mounts,
	}, nil
}

// bindPHP caches Composer packages in a persistent vendor directory.
func (m *DepManager) bindPHP(cdir, workspaceDir string) (*CacheBinding, error) {
	composerCache := filepath.Join(cdir, "composer-cache")
	_ = os.MkdirAll(composerCache, 0o755)
	vendorCache := filepath.Join(cdir, "vendor")
	_ = os.MkdirAll(vendorCache, 0o755)

	var mounts []BindMount
	if workspaceDir != "" {
		mounts = append(mounts, BindMount{
			HostPath:      vendorCache,
			WorkspacePath: "vendor",
		})
	}

	return &CacheBinding{
		Env: []string{
			"COMPOSER_CACHE_DIR=" + composerCache,
			"COMPOSER_VENDOR_DIR=" + vendorCache,
		},
		BindMounts: mounts,
	}, nil
}

// bindPerl caches CPAN modules via local::lib.
func (m *DepManager) bindPerl(cdir string) (*CacheBinding, error) {
	perlLib := filepath.Join(cdir, "perl5")
	_ = os.MkdirAll(perlLib, 0o755)

	return &CacheBinding{
		Env: []string{
			"PERL5LIB=" + filepath.Join(perlLib, "lib", "perl5"),
			"PERL_LOCAL_LIB_ROOT=" + perlLib,
			"PERL_MB_OPT=--install_base " + perlLib,
			"PERL_MM_OPT=INSTALL_BASE=" + perlLib,
		},
	}, nil
}

// bindLua caches LuaRocks modules.
func (m *DepManager) bindLua(cdir string) (*CacheBinding, error) {
	luaRocks := filepath.Join(cdir, "luarocks")
	_ = os.MkdirAll(luaRocks, 0o755)

	return &CacheBinding{
		Env: []string{"LUAROCKS_CONFIG=" + filepath.Join(cdir, "luarocks-config.lua")},
	}, nil
}

// bindR stores R package library in a persistent directory.
func (m *DepManager) bindR(cdir string) (*CacheBinding, error) {
	rLib := filepath.Join(cdir, "r-libs")
	_ = os.MkdirAll(rLib, 0o755)
	rEnvCache := filepath.Join(cdir, "renv-cache")
	_ = os.MkdirAll(rEnvCache, 0o755)

	return &CacheBinding{
		Env: []string{
			"R_LIBS_USER=" + rLib,
			"RENV_PATHS_CACHE=" + rEnvCache,
		},
	}, nil
}

// bindJulia stores the Julia package depot persistently.
func (m *DepManager) bindJulia(cdir string) (*CacheBinding, error) {
	juliaDepot := filepath.Join(cdir, "julia-depot")
	_ = os.MkdirAll(juliaDepot, 0o755)

	return &CacheBinding{
		Env: []string{"JULIA_DEPOT_PATH=" + juliaDepot},
	}, nil
}

// ── Functional ───────────────────────────────────────────────────────────────

// bindElixir caches Hex packages and rebar3 packages (shared with Erlang).
func (m *DepManager) bindElixir(cdir string) (*CacheBinding, error) {
	hexHome := filepath.Join(cdir, "hex-home")
	_ = os.MkdirAll(hexHome, 0o755)
	rebar3Home := filepath.Join(cdir, "rebar3")
	_ = os.MkdirAll(rebar3Home, 0o755)
	mixHome := filepath.Join(cdir, "mix-home")
	_ = os.MkdirAll(mixHome, 0o755)

	return &CacheBinding{
		Env: []string{
			"HEX_HOME=" + hexHome,
			"REBAR3_BASE_DIR=" + rebar3Home,
			"MIX_HOME=" + mixHome,
		},
	}, nil
}

// bindHaskell caches Cabal and Stack packages.
func (m *DepManager) bindHaskell(cdir string) (*CacheBinding, error) {
	cabalDir := filepath.Join(cdir, "cabal")
	_ = os.MkdirAll(cabalDir, 0o755)
	stackRoot := filepath.Join(cdir, "stack")
	_ = os.MkdirAll(stackRoot, 0o755)

	return &CacheBinding{
		Env: []string{
			"CABAL_DIR=" + cabalDir,
			"STACK_ROOT=" + stackRoot,
		},
	}, nil
}

// bindOCaml caches opam switch state.
func (m *DepManager) bindOCaml(cdir string) (*CacheBinding, error) {
	opamRoot := filepath.Join(cdir, "opam")
	_ = os.MkdirAll(opamRoot, 0o755)

	return &CacheBinding{
		Env: []string{"OPAMROOT=" + opamRoot},
	}, nil
}

// bindClojure caches Maven local repository (used by Leiningen and deps.edn).
func (m *DepManager) bindClojure(cdir string) (*CacheBinding, error) {
	m2Repo := filepath.Join(cdir, "m2-repository")
	_ = os.MkdirAll(m2Repo, 0o755)
	clojureCache := filepath.Join(cdir, "clojure-tools")
	_ = os.MkdirAll(clojureCache, 0o755)

	return &CacheBinding{
		Env: []string{
			"MAVEN_OPTS=-Dmaven.repo.local=" + m2Repo,
			"CLJ_CONFIG=" + clojureCache,
		},
	}, nil
}

// ── Mobile / cross-platform ───────────────────────────────────────────────────

// bindSwift caches SwiftPM package checkouts and build products.
func (m *DepManager) bindSwift(cdir string) (*CacheBinding, error) {
	spmCache := filepath.Join(cdir, "spm-cache")
	_ = os.MkdirAll(spmCache, 0o755)

	return &CacheBinding{
		Env: []string{"SWIFT_PACKAGE_CACHE_DIR=" + spmCache},
	}, nil
}

// bindDart caches Dart/Flutter pub packages.
func (m *DepManager) bindDart(cdir string) (*CacheBinding, error) {
	pubCache := filepath.Join(cdir, "pub-cache")
	_ = os.MkdirAll(pubCache, 0o755)

	return &CacheBinding{
		Env: []string{"PUB_CACHE=" + pubCache},
	}, nil
}

// bindDotNet caches NuGet packages.
func (m *DepManager) bindDotNet(cdir string) (*CacheBinding, error) {
	nugetCache := filepath.Join(cdir, "nuget")
	_ = os.MkdirAll(nugetCache, 0o755)
	dotnetRoot := filepath.Join(cdir, "dotnet-tools")
	_ = os.MkdirAll(dotnetRoot, 0o755)

	return &CacheBinding{
		Env: []string{
			"NUGET_PACKAGES=" + nugetCache,
			"DOTNET_ROOT=" + dotnetRoot,
		},
	}, nil
}

// ── Embedded ─────────────────────────────────────────────────────────────────

// bindZephyr points west at a persistent modules directory so `west update`
// does not re-clone all repositories on every build.
func (m *DepManager) bindZephyr(cdir, workspaceDir string) (*CacheBinding, error) {
	westModules := filepath.Join(cdir, "west-modules")
	_ = os.MkdirAll(westModules, 0o755)

	var mounts []BindMount
	if workspaceDir != "" {
		mounts = append(mounts, BindMount{
			HostPath:      westModules,
			WorkspacePath: ".west",
		})
	}

	return &CacheBinding{
		Env: []string{
			"ZEPHYR_BUILD_CACHE=" + filepath.Join(cdir, "cmake-cache"),
		},
		BindMounts: mounts,
	}, nil
}


// ApplyBindMounts materialises the BindMounts for a CacheBinding by creating
// symlinks inside workspaceDir.  This is called by the Executor before running
// the build command on the host runner (Docker runners handle mounts at
// container creation time instead).
func (b *CacheBinding) ApplyBindMounts(workspaceDir string) error {
	for _, bm := range b.BindMounts {
		if bm.WorkspacePath == "" {
			continue
		}
		dest := filepath.Join(workspaceDir, filepath.FromSlash(bm.WorkspacePath))
		// Only create the symlink if the destination does not already exist.
		if _, err := os.Lstat(dest); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return fmt.Errorf("ApplyBindMounts mkdir: %w", err)
			}
			if err := os.Symlink(bm.HostPath, dest); err != nil && !os.IsExist(err) {
				return fmt.Errorf("ApplyBindMounts symlink %s→%s: %w", bm.HostPath, dest, err)
			}
		}
	}
	return nil
}

// EnvSlice returns the environment variable slice to pass to exec.Cmd.Env.
func (b *CacheBinding) EnvSlice() []string {
	return b.Env
}
