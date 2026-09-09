package cache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/debaucheryparty/packets/pkg/apitypes"
)

type DepManager struct {
	cacheRoot string
}

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

func NewDepManagerFromEnv() *DepManager {
	return NewDepManager(os.Getenv("PACKETS_DEP_CACHE_ROOT"))
}

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

type CacheBinding struct {
	Env []string

	BindMounts []BindMount
}

type BindMount struct {
	HostPath string

	WorkspacePath string
}

func (m *DepManager) Bind(_ context.Context, owner string, toolchain apitypes.Toolchain, workspaceDir string) (*CacheBinding, error) {
	cdir := m.cacheDir(owner, toolchain)

	switch toolchain {

	case apitypes.ToolchainAndroid, apitypes.ToolchainKotlin,
		apitypes.ToolchainJava, apitypes.ToolchainScala,
		apitypes.ToolchainGroovy:
		return m.bindGradle(cdir, workspaceDir, toolchain)

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

	case apitypes.ToolchainElixir, apitypes.ToolchainErlang:
		return m.bindElixir(cdir)

	case apitypes.ToolchainHaskell:
		return m.bindHaskell(cdir)

	case apitypes.ToolchainOCaml:
		return m.bindOCaml(cdir)

	case apitypes.ToolchainClojure:
		return m.bindClojure(cdir)

	case apitypes.ToolchainSwift, apitypes.ToolchainObjC:
		return m.bindSwift(cdir)

	case apitypes.ToolchainDart, apitypes.ToolchainFlutter:
		return m.bindDart(cdir)

	case apitypes.ToolchainDotNet:
		return m.bindDotNet(cdir)

	case apitypes.ToolchainZephyr:
		return m.bindZephyr(cdir, workspaceDir)

	default:
		return &CacheBinding{}, nil
	}
}

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

func (m *DepManager) bindC(cdir string) (*CacheBinding, error) {
	ccacheDir := filepath.Join(cdir, "ccache")
	_ = os.MkdirAll(ccacheDir, 0o755)

	return &CacheBinding{
		Env: []string{
			"CCACHE_DIR=" + ccacheDir,

			"CMAKE_C_COMPILER_LAUNCHER=ccache",
			"CMAKE_CXX_COMPILER_LAUNCHER=ccache",
		},
	}, nil
}

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

func (m *DepManager) bindNim(cdir string) (*CacheBinding, error) {
	nimbleDir := filepath.Join(cdir, "nimble")
	_ = os.MkdirAll(nimbleDir, 0o755)

	return &CacheBinding{
		Env: []string{"NIMBLE_DIR=" + nimbleDir},
	}, nil
}

func (m *DepManager) bindD(cdir string) (*CacheBinding, error) {
	dubCache := filepath.Join(cdir, "dub")
	_ = os.MkdirAll(dubCache, 0o755)

	return &CacheBinding{
		Env: []string{"DUB_HOME=" + dubCache},
	}, nil
}

func (m *DepManager) bindV(cdir string) (*CacheBinding, error) {
	vModules := filepath.Join(cdir, "vmodules")
	_ = os.MkdirAll(vModules, 0o755)

	return &CacheBinding{
		Env: []string{"VMODULES=" + vModules},
	}, nil
}

func (m *DepManager) bindCrystal(cdir string) (*CacheBinding, error) {
	shardsCache := filepath.Join(cdir, "shards-cache")
	_ = os.MkdirAll(shardsCache, 0o755)

	return &CacheBinding{
		Env: []string{"SHARDS_CACHE_PATH=" + shardsCache},
	}, nil
}

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

func (m *DepManager) bindLua(cdir string) (*CacheBinding, error) {
	luaRocks := filepath.Join(cdir, "luarocks")
	_ = os.MkdirAll(luaRocks, 0o755)

	return &CacheBinding{
		Env: []string{"LUAROCKS_CONFIG=" + filepath.Join(cdir, "luarocks-config.lua")},
	}, nil
}

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

func (m *DepManager) bindJulia(cdir string) (*CacheBinding, error) {
	juliaDepot := filepath.Join(cdir, "julia-depot")
	_ = os.MkdirAll(juliaDepot, 0o755)

	return &CacheBinding{
		Env: []string{"JULIA_DEPOT_PATH=" + juliaDepot},
	}, nil
}

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

func (m *DepManager) bindOCaml(cdir string) (*CacheBinding, error) {
	opamRoot := filepath.Join(cdir, "opam")
	_ = os.MkdirAll(opamRoot, 0o755)

	return &CacheBinding{
		Env: []string{"OPAMROOT=" + opamRoot},
	}, nil
}

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

func (m *DepManager) bindSwift(cdir string) (*CacheBinding, error) {
	spmCache := filepath.Join(cdir, "spm-cache")
	_ = os.MkdirAll(spmCache, 0o755)

	return &CacheBinding{
		Env: []string{"SWIFT_PACKAGE_CACHE_DIR=" + spmCache},
	}, nil
}

func (m *DepManager) bindDart(cdir string) (*CacheBinding, error) {
	pubCache := filepath.Join(cdir, "pub-cache")
	_ = os.MkdirAll(pubCache, 0o755)

	return &CacheBinding{
		Env: []string{"PUB_CACHE=" + pubCache},
	}, nil
}

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

func (b *CacheBinding) ApplyBindMounts(workspaceDir string) error {
	for _, bm := range b.BindMounts {
		if bm.WorkspacePath == "" {
			continue
		}
		dest := filepath.Join(workspaceDir, filepath.FromSlash(bm.WorkspacePath))

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

func (b *CacheBinding) EnvSlice() []string {
	return b.Env
}
