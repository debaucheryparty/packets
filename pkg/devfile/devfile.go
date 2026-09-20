package devfile

type Devfile struct {
	Name        string            `yaml:"name" json:"name"`
	Version     string            `yaml:"version,omitempty" json:"version,omitempty"`
	Environment EnvironmentSpec   `yaml:"environment,omitempty" json:"environment,omitempty"`
	Services    []ServiceSpec     `yaml:"services,omitempty" json:"services,omitempty"`
	Build       CommandSpec       `yaml:"build,omitempty" json:"build,omitempty"`
	Test        CommandSpec       `yaml:"test,omitempty" json:"test,omitempty"`
	Ports       []int             `yaml:"ports,omitempty" json:"ports,omitempty"`
	Metadata    map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

type EnvironmentSpec struct {
	Java    string            `yaml:"java,omitempty" json:"java,omitempty"`
	Android AndroidSpec       `yaml:"android,omitempty" json:"android,omitempty"`
	Rust    string            `yaml:"rust,omitempty" json:"rust,omitempty"`
	Go      string            `yaml:"go,omitempty" json:"go,omitempty"`
	Node    string            `yaml:"node,omitempty" json:"node,omitempty"`
	Python  string            `yaml:"python,omitempty" json:"python,omitempty"`
	Custom  map[string]string `yaml:"custom,omitempty" json:"custom,omitempty"`
}

type AndroidSpec struct {
	SDK           string `yaml:"sdk,omitempty" json:"sdk,omitempty"`
	BuildTools    string `yaml:"build_tools,omitempty" json:"build_tools,omitempty"`
	NDK           string `yaml:"ndk,omitempty" json:"ndk,omitempty"`
	StartEmulator bool   `yaml:"start_emulator,omitempty" json:"start_emulator,omitempty"`
}

type ServiceSpec struct {
	Name        string            `yaml:"name" json:"name"`
	Image       string            `yaml:"image,omitempty" json:"image,omitempty"`
	Command     string            `yaml:"command,omitempty" json:"command,omitempty"`
	Environment map[string]string `yaml:"environment,omitempty" json:"environment,omitempty"`
	Ports       []string          `yaml:"ports,omitempty" json:"ports,omitempty"`
}

type CommandSpec struct {
	Command string            `yaml:"command" json:"command"`
	Args    []string          `yaml:"args,omitempty" json:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}
