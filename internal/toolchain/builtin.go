package toolchain

import "github.com/debaucheryparty/packets/pkg/apitypes"

func registerBuiltins(r *Registry) {
	for _, def := range builtinToolchains {
		r.Register(def)
	}
}

var builtinToolchains = []apitypes.ToolchainDef{
	{Name: apitypes.ToolchainRust, DisplayName: "Rust", DetectFiles: []string{"Cargo.toml"}, Backend: apitypes.BackendSccacheDist, LocalCommand: "cargo", DefaultArgs: []string{"build"}, DockerImage: "rust:latest", DefaultArtifacts: []string{"target/release/**", "target/debug/**"}},
	{Name: apitypes.ToolchainCPP, DisplayName: "C++", DetectFiles: []string{"CMakeLists.txt"}, Backend: apitypes.BackendSccacheDist, LocalCommand: "cmake", DefaultArgs: []string{"--build", "."}, DockerImage: "gcc:latest", DefaultArtifacts: []string{"build/**", "bin/**"}},
	{Name: apitypes.ToolchainC, DisplayName: "C", DetectFiles: []string{"Makefile", "configure"}, Backend: apitypes.BackendSccacheDist, LocalCommand: "make", DockerImage: "gcc:latest", DefaultArtifacts: []string{"build/**", "bin/**", "out/**"}},

	{Name: apitypes.ToolchainKotlin, DisplayName: "Kotlin", DetectFiles: []string{"build.gradle.kts"}, Backend: apitypes.BackendGradleCache, LocalCommand: "gradle", DefaultArgs: []string{"build"}, DockerImage: "gradle:latest", DefaultArtifacts: []string{"build/libs/**"}},
	{Name: apitypes.ToolchainScala, DisplayName: "Scala", DetectFiles: []string{"build.sbt"}, Backend: apitypes.BackendGradleCache, LocalCommand: "sbt", DefaultArgs: []string{"compile"}, DockerImage: "sbtscala/scala-sbt:latest", DefaultArtifacts: []string{"target/**"}},
	{Name: apitypes.ToolchainJava, DisplayName: "Java", DetectFiles: []string{"pom.xml"}, Backend: apitypes.BackendGradleCache, LocalCommand: "mvn", DefaultArgs: []string{"compile"}, DockerImage: "maven:latest", DefaultArtifacts: []string{"target/*.jar", "target/*.war"}},
	{Name: apitypes.ToolchainGroovy, DisplayName: "Groovy", DetectFiles: []string{"build.gradle"}, Backend: apitypes.BackendGradleCache, LocalCommand: "gradle", DefaultArgs: []string{"build"}, DockerImage: "gradle:latest", DefaultArtifacts: []string{"build/libs/**"}},

	{Name: apitypes.ToolchainSwift, DisplayName: "Swift", DetectFiles: []string{"Package.swift"}, Backend: apitypes.BackendCIProvider, LocalCommand: "swift", DefaultArgs: []string{"build"}, DefaultArtifacts: []string{".build/release/**", ".build/debug/**"}},
	{Name: apitypes.ToolchainObjC, DisplayName: "Objective-C", DetectDirs: []string{"*.xcodeproj", "*.xcworkspace"}, Backend: apitypes.BackendCIProvider, LocalCommand: "xcodebuild", DefaultArtifacts: []string{"build/**"}},
	{Name: apitypes.ToolchainFlutter, DisplayName: "Flutter", DetectFiles: []string{"pubspec.yaml"}, DetectDirs: []string{"ios", "android"}, Backend: apitypes.BackendCIProvider, LocalCommand: "flutter", DefaultArgs: []string{"build"}, DefaultArtifacts: []string{"build/app/outputs/**"}},

	{Name: apitypes.ToolchainGo, DisplayName: "Go", DetectFiles: []string{"go.mod"}, Backend: apitypes.BackendScheduler, LocalCommand: "go", DefaultArgs: []string{"build", "./..."}, DockerImage: "golang:latest", DefaultArtifacts: []string{"bin/**", "dist/**", "*.exe"}},
	{Name: apitypes.ToolchainPython, DisplayName: "Python", DetectFiles: []string{"pyproject.toml"}, Backend: apitypes.BackendScheduler, LocalCommand: "python", DefaultArgs: []string{"-m", "build"}, DockerImage: "python:latest", DefaultArtifacts: []string{"dist/**", "build/**"}},
	{Name: apitypes.ToolchainNode, DisplayName: "Node.js", DetectFiles: []string{"package.json"}, Backend: apitypes.BackendScheduler, LocalCommand: "npm", DefaultArgs: []string{"run", "build"}, DockerImage: "node:latest", DefaultArtifacts: []string{"dist/**", "build/**", "out/**"}},
	{Name: apitypes.ToolchainRuby, DisplayName: "Ruby", DetectFiles: []string{"Gemfile"}, Backend: apitypes.BackendScheduler, LocalCommand: "bundle", DefaultArgs: []string{"exec", "rake", "build"}, DockerImage: "ruby:latest", DefaultArtifacts: []string{"pkg/**"}},
	{Name: apitypes.ToolchainPHP, DisplayName: "PHP", DetectFiles: []string{"composer.json"}, Backend: apitypes.BackendScheduler, LocalCommand: "composer", DefaultArgs: []string{"install"}, DockerImage: "php:latest", DefaultArtifacts: []string{"vendor/**"}},
	{Name: apitypes.ToolchainElixir, DisplayName: "Elixir", DetectFiles: []string{"mix.exs"}, Backend: apitypes.BackendScheduler, LocalCommand: "mix", DefaultArgs: []string{"compile"}, DockerImage: "elixir:latest", DefaultArtifacts: []string{"_build/**"}},
	{Name: apitypes.ToolchainHaskell, DisplayName: "Haskell", DetectFiles: []string{"stack.yaml", "cabal.project"}, Backend: apitypes.BackendScheduler, LocalCommand: "cabal", DefaultArgs: []string{"build"}, DockerImage: "haskell:latest", DefaultArtifacts: []string{"dist-newstyle/**"}},
	{Name: apitypes.ToolchainZig, DisplayName: "Zig", DetectFiles: []string{"build.zig"}, Backend: apitypes.BackendScheduler, LocalCommand: "zig", DefaultArgs: []string{"build"}, DockerImage: "euantorano/zig:latest", DefaultArtifacts: []string{"zig-out/**"}},
	{Name: apitypes.ToolchainNim, DisplayName: "Nim", DetectFiles: []string{"nim.cfg", "config.nims"}, Backend: apitypes.BackendScheduler, LocalCommand: "nimble", DefaultArgs: []string{"build"}, DockerImage: "nimlang/nim:latest", DefaultArtifacts: []string{"bin/**"}},
	{Name: apitypes.ToolchainOCaml, DisplayName: "OCaml", DetectFiles: []string{"dune-project"}, Backend: apitypes.BackendScheduler, LocalCommand: "dune", DefaultArgs: []string{"build"}, DockerImage: "ocaml/opam:latest", DefaultArtifacts: []string{"_build/**"}},
	{Name: apitypes.ToolchainDart, DisplayName: "Dart", DetectFiles: []string{"pubspec.yaml"}, Backend: apitypes.BackendScheduler, LocalCommand: "dart", DefaultArgs: []string{"compile", "exe"}, DockerImage: "dart:latest", DefaultArtifacts: []string{"build/**"}},
	{Name: apitypes.ToolchainDotNet, DisplayName: ".NET", DetectFiles: []string{"Directory.Build.props", "global.json"}, Backend: apitypes.BackendScheduler, LocalCommand: "dotnet", DefaultArgs: []string{"build"}, DockerImage: "mcr.microsoft.com/dotnet/sdk:latest", DefaultArtifacts: []string{"bin/Release/**", "bin/Debug/**"}},
	{Name: apitypes.ToolchainCrystal, DisplayName: "Crystal", DetectFiles: []string{"shard.yml"}, Backend: apitypes.BackendScheduler, LocalCommand: "shards", DefaultArgs: []string{"build"}, DockerImage: "crystallang/crystal:latest", DefaultArtifacts: []string{"bin/**"}},
	{Name: apitypes.ToolchainClojure, DisplayName: "Clojure", DetectFiles: []string{"deps.edn", "project.clj"}, Backend: apitypes.BackendScheduler, LocalCommand: "clj", DefaultArgs: []string{"-M:build"}, DockerImage: "clojure:latest", DefaultArtifacts: []string{"target/**"}},
	{Name: apitypes.ToolchainPerl, DisplayName: "Perl", DetectFiles: []string{"cpanfile", "Makefile.PL"}, Backend: apitypes.BackendScheduler, LocalCommand: "cpanm", DefaultArgs: []string{"--installdeps", "."}, DockerImage: "perl:latest", DefaultArtifacts: []string{"blib/**"}},
	{Name: apitypes.ToolchainLua, DisplayName: "Lua", DetectFiles: []string{".luacheckrc", "luarocks.lock"}, Backend: apitypes.BackendScheduler, LocalCommand: "luarocks", DefaultArgs: []string{"build"}, DockerImage: "nickblah/lua:latest", DefaultArtifacts: []string{"build/**"}},
	{Name: apitypes.ToolchainR, DisplayName: "R", DetectFiles: []string{"DESCRIPTION"}, Backend: apitypes.BackendScheduler, LocalCommand: "Rscript", DefaultArgs: []string{"-e", "devtools::build()"}, DockerImage: "r-base:latest", DefaultArtifacts: []string{"*.tar.gz"}},
	{Name: apitypes.ToolchainJulia, DisplayName: "Julia", DetectFiles: []string{"Project.toml"}, Backend: apitypes.BackendScheduler, LocalCommand: "julia", DefaultArgs: []string{"--project=.", "-e", "using Pkg; Pkg.build()"}, DockerImage: "julia:latest", DefaultArtifacts: []string{"build/**"}},
	{Name: apitypes.ToolchainD, DisplayName: "D", DetectFiles: []string{"dub.json", "dub.sdl"}, Backend: apitypes.BackendScheduler, LocalCommand: "dub", DefaultArgs: []string{"build"}, DockerImage: "dlang2/dmd-ubuntu:latest", DefaultArtifacts: []string{"bin/**"}},
	{Name: apitypes.ToolchainV, DisplayName: "V", DetectFiles: []string{"v.mod"}, Backend: apitypes.BackendScheduler, LocalCommand: "v", DefaultArgs: []string{"."}, DockerImage: "thevlang/vlang:latest", DefaultArtifacts: []string{"bin/**"}},
	{Name: apitypes.ToolchainErlang, DisplayName: "Erlang", DetectFiles: []string{"rebar.config"}, Backend: apitypes.BackendScheduler, LocalCommand: "rebar3", DefaultArgs: []string{"compile"}, DockerImage: "erlang:latest", DefaultArtifacts: []string{"_build/**"}},
	{
		Name:             apitypes.ToolchainAndroid,
		DisplayName:      "Android",
		DetectFiles:      []string{"gradlew"},
		DetectDirs:       []string{"app"},
		Backend:          apitypes.BackendScheduler,
		LocalCommand:     "./gradlew",
		DefaultArgs:      []string{"assembleDebug"},
		DockerImage:      "reactnativecommunity/react-native-android:latest",
		DefaultArtifacts: []string{"app/build/outputs/apk/debug/*.apk"},
	},
}
