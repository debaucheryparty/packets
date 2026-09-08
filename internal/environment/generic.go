package environment

import (
	"context"
)

type GenericResolver struct{}

func NewGenericResolver() *GenericResolver {
	return &GenericResolver{}
}

func (g *GenericResolver) Check(ctx context.Context, comp Component) []ToolchainRequirement {
	switch comp.Type {
	case ComponentRust:
		return []ToolchainRequirement{
			checkTool("Rust compiler (rustc)", "rustc", "--version", "curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh -s -- -y"),
			checkTool("Cargo", "cargo", "--version", "rustup update"),
		}
	case ComponentGo:
		return []ToolchainRequirement{
			checkTool("Go compiler", "go", "version", "apt-get install -y golang-go"),
		}
	case ComponentNode:
		pm := comp.Metadata["package_manager"]
		if pm == "" {
			pm = "npm"
		}
		return []ToolchainRequirement{
			checkTool("Node.js", "node", "--version", "apt-get install -y nodejs"),
			checkTool(pm, pm, "--version", "npm install -g "+pm),
		}
	case ComponentCMake:
		return []ToolchainRequirement{
			checkTool("CMake", "cmake", "--version", "apt-get install -y cmake"),
			checkTool("Ninja", "ninja", "--version", "apt-get install -y ninja-build"),
		}
	case ComponentPython:
		pm := comp.Metadata["package_manager"]
		reqs := []ToolchainRequirement{
			checkTool("Python 3", "python3", "--version", "apt-get install -y python3 python3-pip python3-venv"),
			checkTool("Pip", "pip3", "--version", "apt-get install -y python3-pip"),
		}
		if pm == "poetry" {
			reqs = append(reqs, checkTool("Poetry", "poetry", "--version", "pip3 install poetry"))
		} else if pm == "pipenv" {
			reqs = append(reqs, checkTool("Pipenv", "pipenv", "--version", "pip3 install pipenv"))
		}
		return reqs
	case ComponentJava:
		reqs := []ToolchainRequirement{
			checkTool("Java Development Kit (JDK)", "javac", "-version", "apt-get install -y openjdk-17-jdk"),
		}
		bs := comp.Metadata["build_system"]
		if bs == "maven" || bs == "" {
			reqs = append(reqs, checkTool("Maven", "mvn", "-version", "apt-get install -y maven"))
		}
		if bs == "gradle" {
			reqs = append(reqs, checkTool("Gradle", "gradle", "-version", "apt-get install -y gradle"))
		}
		return reqs
	case ComponentSwift:
		return []ToolchainRequirement{
			checkTool("Swift toolchain", "swift", "--version", "apt-get install -y swift"),
		}
	case ComponentRuby:
		return []ToolchainRequirement{
			checkTool("Ruby", "ruby", "--version", "apt-get install -y ruby ruby-dev"),
			checkTool("Bundler", "bundle", "--version", "gem install bundler"),
		}
	case ComponentPHP:
		return []ToolchainRequirement{
			checkTool("PHP CLI", "php", "--version", "apt-get install -y php-cli"),
			checkTool("Composer", "composer", "--version", "apt-get install -y composer"),
		}
	case ComponentZig:
		return []ToolchainRequirement{
			checkTool("Zig compiler", "zig", "version", "snap install zig --classic --beta"),
		}
	case ComponentDotNet:
		return []ToolchainRequirement{
			checkTool(".NET SDK", "dotnet", "--version", "apt-get install -y dotnet-sdk-8.0"),
		}
	case ComponentFlutter:
		return []ToolchainRequirement{
			checkTool("Flutter SDK", "flutter", "--version", "git clone -b stable https://github.com/flutter/flutter.git /opt/flutter && ln -s /opt/flutter/bin/flutter /usr/local/bin/flutter"),
		}
	case ComponentDart:
		return []ToolchainRequirement{
			checkTool("Dart SDK", "dart", "--version", "apt-get install -y dart"),
		}
	case ComponentElixir:
		return []ToolchainRequirement{
			checkTool("Erlang runtime", "erl", "+V", "apt-get install -y erlang"),
			checkTool("Elixir / Mix", "mix", "--version", "apt-get install -y elixir"),
		}
	default:
		return nil
	}
}
