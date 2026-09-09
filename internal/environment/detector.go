package environment

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/debaucheryparty/packets/internal/project"
)

type Detector struct{}

func NewDetector() *Detector {
	return &Detector{}
}

func (d *Detector) DetectTopology(root string) (*ProjectTopology, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("detect topology abs path: %w", err)
	}

	topology := &ProjectTopology{
		RootPath: absRoot,
	}

	if cfg, err := project.LoadConfig(absRoot); err == nil && len(cfg.Components) > 0 {
		topology.IsHybrid = len(cfg.Components) > 1
		for _, cc := range cfg.Components {
			name := cc.Name
			if name == "" {
				name = formatComponentName(cc.Type, cc.Path)
			}
			meta := cc.Metadata
			if meta == nil {
				meta = make(map[string]string)
			}
			topology.Components = append(topology.Components, Component{
				Type:       ComponentType(cc.Type),
				Name:       name,
				Path:       cc.Path,
				Confidence: "manual",
				Metadata:   meta,
			})
		}
		return topology, nil
	}

	comps, err := d.detectInDir(absRoot, "")
	if err != nil {
		return nil, err
	}
	topology.Components = append(topology.Components, comps...)

	entries, err := os.ReadDir(absRoot)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "build" || name == "target" {
				continue
			}
			subDir := filepath.Join(absRoot, name)
			subComps, err := d.detectInDir(subDir, name)
			if err == nil && len(subComps) > 0 {
				for _, sc := range subComps {
					if !topologyHasComponent(topology.Components, sc) {
						topology.Components = append(topology.Components, sc)
					}
				}
			}
		}
	}

	topology.IsHybrid = len(topology.Components) > 1
	return topology, nil
}

func topologyHasComponent(list []Component, c Component) bool {
	for _, item := range list {
		if item.Type == c.Type && item.Path == c.Path {
			return true
		}
	}
	return false
}

func (d *Detector) detectInDir(dir, relPath string) ([]Component, error) {
	var comps []Component

	androidComp := d.detectAndroid(dir, relPath)
	if androidComp != nil {
		comps = append(comps, *androidComp)
	}

	if zephyrComp := d.detectZephyr(dir, relPath); zephyrComp != nil {
		comps = append(comps, *zephyrComp)
	}

	if rustComp := d.detectRust(dir, relPath); rustComp != nil {
		comps = append(comps, *rustComp)
	}

	if goComp := d.detectGo(dir, relPath); goComp != nil {
		comps = append(comps, *goComp)
	}

	if nodeComp := d.detectNode(dir, relPath); nodeComp != nil {
		comps = append(comps, *nodeComp)
	}

	if pythonComp := d.detectPython(dir, relPath); pythonComp != nil {
		comps = append(comps, *pythonComp)
	}

	if javaComp := d.detectJava(dir, relPath, androidComp != nil); javaComp != nil {
		comps = append(comps, *javaComp)
	}

	if swiftComp := d.detectSwift(dir, relPath); swiftComp != nil {
		comps = append(comps, *swiftComp)
	}

	if rubyComp := d.detectRuby(dir, relPath); rubyComp != nil {
		comps = append(comps, *rubyComp)
	}

	if phpComp := d.detectPHP(dir, relPath); phpComp != nil {
		comps = append(comps, *phpComp)
	}

	if zigComp := d.detectZig(dir, relPath); zigComp != nil {
		comps = append(comps, *zigComp)
	}

	if dotnetComp := d.detectDotNet(dir, relPath); dotnetComp != nil {
		comps = append(comps, *dotnetComp)
	}

	if flutterComp := d.detectFlutterOrDart(dir, relPath); flutterComp != nil {
		comps = append(comps, *flutterComp)
	}

	if elixirComp := d.detectElixir(dir, relPath); elixirComp != nil {
		comps = append(comps, *elixirComp)
	}

	if cmakeComp := d.detectCMake(dir, relPath); cmakeComp != nil {

		hasZephyrOrAndroid := false
		for _, c := range comps {
			if c.Type == ComponentZephyr || c.Type == ComponentAndroid {
				hasZephyrOrAndroid = true
				break
			}
		}
		if !hasZephyrOrAndroid {
			comps = append(comps, *cmakeComp)
		}
	}

	return comps, nil
}

func (d *Detector) detectAndroid(dir, rel string) *Component {
	hasGradlew := fileExists(filepath.Join(dir, "gradlew")) || fileExists(filepath.Join(dir, "gradlew.bat"))
	hasSettings := fileExists(filepath.Join(dir, "settings.gradle")) || fileExists(filepath.Join(dir, "settings.gradle.kts"))
	hasBuild := fileExists(filepath.Join(dir, "build.gradle")) || fileExists(filepath.Join(dir, "build.gradle.kts"))
	hasApp := dirExists(filepath.Join(dir, "app"))

	if !hasGradlew && !hasSettings && !hasBuild {
		return nil
	}

	isAndroid := hasApp || fileExists(filepath.Join(dir, "src", "main", "AndroidManifest.xml")) || fileExists(filepath.Join(dir, "AndroidManifest.xml"))
	if !isAndroid {
		for _, bg := range []string{
			filepath.Join(dir, "build.gradle.kts"),
			filepath.Join(dir, "build.gradle"),
		} {
			if data, err := os.ReadFile(bg); err == nil {
				s := string(data)
				if strings.Contains(s, "com.android") || strings.Contains(s, "android {") || strings.Contains(s, "compileSdk") {
					isAndroid = true
					break
				}
			}
		}
	}
	if !isAndroid {
		return nil
	}

	meta := make(map[string]string)
	meta["gradle_wrapper"] = "false"

	wrapperProps := filepath.Join(dir, "gradle", "wrapper", "gradle-wrapper.properties")
	if fileExists(wrapperProps) {
		meta["gradle_wrapper"] = "true"
		if ver := extractGradleVersion(wrapperProps); ver != "" {
			meta["gradle_version"] = ver
		}
	}

	for _, bg := range []string{
		filepath.Join(dir, "app", "build.gradle.kts"),
		filepath.Join(dir, "app", "build.gradle"),
		filepath.Join(dir, "build.gradle.kts"),
		filepath.Join(dir, "build.gradle"),
	} {
		if fileExists(bg) {
			if sdk := extractCompileSdk(bg); sdk != "" {
				meta["compile_sdk"] = sdk
				break
			}
		}
	}

	confidence := "medium"
	if hasGradlew && (hasSettings || hasApp) {
		confidence = "high"
	}

	name := "Android"
	if rel != "" {
		name = fmt.Sprintf("Android (%s)", rel)
	}

	return &Component{
		Type:       ComponentAndroid,
		Name:       name,
		Path:       rel,
		Confidence: confidence,
		Metadata:   meta,
	}
}

func (d *Detector) detectZephyr(dir, rel string) *Component {
	hasWest := fileExists(filepath.Join(dir, "west.yml")) || fileExists(filepath.Join(dir, ".west", "config"))
	hasPrjConf := fileExists(filepath.Join(dir, "prj.conf"))
	hasCMake := fileExists(filepath.Join(dir, "CMakeLists.txt"))

	if !hasWest && !(hasPrjConf && hasCMake) {
		return nil
	}

	meta := make(map[string]string)
	if hasWest {
		meta["manifest"] = "west.yml"
	}
	if hasPrjConf {
		meta["prj_conf"] = "prj.conf"
	}

	confidence := "medium"
	if hasWest {
		confidence = "high"
	}

	name := "Zephyr RTOS"
	if rel != "" {
		name = fmt.Sprintf("Zephyr (%s)", rel)
	}

	return &Component{
		Type:       ComponentZephyr,
		Name:       name,
		Path:       rel,
		Confidence: confidence,
		Metadata:   meta,
	}
}

func (d *Detector) detectRust(dir, rel string) *Component {
	cargoToml := filepath.Join(dir, "Cargo.toml")
	if !fileExists(cargoToml) {
		return nil
	}

	meta := make(map[string]string)
	for _, tc := range []string{"rust-toolchain.toml", "rust-toolchain"} {
		if fileExists(filepath.Join(dir, tc)) {
			meta["toolchain_file"] = tc
			break
		}
	}

	name := "Rust"
	if rel != "" {
		name = fmt.Sprintf("Rust (%s)", rel)
	}

	return &Component{
		Type:       ComponentRust,
		Name:       name,
		Path:       rel,
		Confidence: "high",
		Metadata:   meta,
	}
}

func (d *Detector) detectGo(dir, rel string) *Component {
	goMod := filepath.Join(dir, "go.mod")
	if !fileExists(goMod) {
		return nil
	}

	meta := make(map[string]string)
	if data, err := os.ReadFile(goMod); err == nil {
		re := regexp.MustCompile(`go\s+([0-9]+\.[0-9]+)`)
		if matches := re.FindStringSubmatch(string(data)); len(matches) > 1 {
			meta["go_version"] = matches[1]
		}
	}

	name := "Go"
	if rel != "" {
		name = fmt.Sprintf("Go (%s)", rel)
	}

	return &Component{
		Type:       ComponentGo,
		Name:       name,
		Path:       rel,
		Confidence: "high",
		Metadata:   meta,
	}
}

func (d *Detector) detectNode(dir, rel string) *Component {
	pkgJson := filepath.Join(dir, "package.json")
	if !fileExists(pkgJson) {
		return nil
	}

	meta := make(map[string]string)
	if fileExists(filepath.Join(dir, "pnpm-lock.yaml")) {
		meta["package_manager"] = "pnpm"
	} else if fileExists(filepath.Join(dir, "yarn.lock")) {
		meta["package_manager"] = "yarn"
	} else {
		meta["package_manager"] = "npm"
	}

	name := "Node.js"
	if rel != "" {
		name = fmt.Sprintf("Node (%s)", rel)
	}

	return &Component{
		Type:       ComponentNode,
		Name:       name,
		Path:       rel,
		Confidence: "high",
		Metadata:   meta,
	}
}

func (d *Detector) detectPython(dir, rel string) *Component {
	hasReqs := fileExists(filepath.Join(dir, "requirements.txt"))
	hasPyproject := fileExists(filepath.Join(dir, "pyproject.toml"))
	hasSetup := fileExists(filepath.Join(dir, "setup.py")) || fileExists(filepath.Join(dir, "setup.cfg"))
	hasPipfile := fileExists(filepath.Join(dir, "Pipfile")) || fileExists(filepath.Join(dir, "Pipfile.lock"))
	hasPoetry := fileExists(filepath.Join(dir, "poetry.lock"))

	if !hasReqs && !hasPyproject && !hasSetup && !hasPipfile && !hasPoetry {
		return nil
	}

	meta := make(map[string]string)
	pm := "pip"
	if hasPoetry {
		pm = "poetry"
	} else if hasPipfile {
		pm = "pipenv"
	} else if hasPyproject {
		if content, err := os.ReadFile(filepath.Join(dir, "pyproject.toml")); err == nil {
			s := string(content)
			if strings.Contains(s, "[tool.poetry]") {
				pm = "poetry"
			} else if strings.Contains(s, "[tool.flit]") {
				pm = "flit"
			} else if strings.Contains(s, "[tool.hatch]") {
				pm = "hatch"
			}
		}
	}
	meta["package_manager"] = pm

	name := "Python"
	if rel != "" {
		name = fmt.Sprintf("Python (%s)", rel)
	}

	return &Component{
		Type:       ComponentPython,
		Name:       name,
		Path:       rel,
		Confidence: "high",
		Metadata:   meta,
	}
}

func (d *Detector) detectCMake(dir, rel string) *Component {
	cmake := filepath.Join(dir, "CMakeLists.txt")
	if !fileExists(cmake) {
		return nil
	}

	name := "CMake"
	if rel != "" {
		name = fmt.Sprintf("CMake (%s)", rel)
	}

	return &Component{
		Type:       ComponentCMake,
		Name:       name,
		Path:       rel,
		Confidence: "medium",
	}
}

func (d *Detector) detectJava(dir, rel string, isAndroid bool) *Component {
	if isAndroid {
		return nil
	}
	hasPom := fileExists(filepath.Join(dir, "pom.xml"))
	hasBuildGradle := fileExists(filepath.Join(dir, "build.gradle")) || fileExists(filepath.Join(dir, "build.gradle.kts"))
	if !hasPom && !hasBuildGradle {
		return nil
	}
	meta := make(map[string]string)
	if hasPom {
		meta["build_system"] = "maven"
	} else {
		meta["build_system"] = "gradle"
	}
	name := "Java"
	if rel != "" {
		name = fmt.Sprintf("Java (%s)", rel)
	}
	return &Component{
		Type:       ComponentJava,
		Name:       name,
		Path:       rel,
		Confidence: "high",
		Metadata:   meta,
	}
}

func (d *Detector) detectSwift(dir, rel string) *Component {
	if !fileExists(filepath.Join(dir, "Package.swift")) {
		return nil
	}
	name := "Swift"
	if rel != "" {
		name = fmt.Sprintf("Swift (%s)", rel)
	}
	return &Component{
		Type:       ComponentSwift,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func (d *Detector) detectRuby(dir, rel string) *Component {
	hasGemfile := fileExists(filepath.Join(dir, "Gemfile"))
	hasRubyVer := fileExists(filepath.Join(dir, ".ruby-version"))
	if !hasGemfile && !hasRubyVer {
		return nil
	}
	name := "Ruby"
	if rel != "" {
		name = fmt.Sprintf("Ruby (%s)", rel)
	}
	return &Component{
		Type:       ComponentRuby,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func (d *Detector) detectPHP(dir, rel string) *Component {
	if !fileExists(filepath.Join(dir, "composer.json")) {
		return nil
	}
	name := "PHP"
	if rel != "" {
		name = fmt.Sprintf("PHP (%s)", rel)
	}
	return &Component{
		Type:       ComponentPHP,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func (d *Detector) detectZig(dir, rel string) *Component {
	if !fileExists(filepath.Join(dir, "build.zig")) {
		return nil
	}
	name := "Zig"
	if rel != "" {
		name = fmt.Sprintf("Zig (%s)", rel)
	}
	return &Component{
		Type:       ComponentZig,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func (d *Detector) detectDotNet(dir, rel string) *Component {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	hasDotNet := false
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".csproj") || strings.HasSuffix(n, ".fsproj") || strings.HasSuffix(n, ".sln") ||
			n == "global.json" || n == "Directory.Build.props" {
			hasDotNet = true
			break
		}
	}
	if !hasDotNet {
		return nil
	}
	name := ".NET"
	if rel != "" {
		name = fmt.Sprintf(".NET (%s)", rel)
	}
	return &Component{
		Type:       ComponentDotNet,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func (d *Detector) detectFlutterOrDart(dir, rel string) *Component {
	pubspec := filepath.Join(dir, "pubspec.yaml")
	if !fileExists(pubspec) {
		return nil
	}
	isFlutter := false
	if content, err := os.ReadFile(pubspec); err == nil {
		s := string(content)
		if strings.Contains(s, "sdk: flutter") || strings.Contains(s, "flutter:") {
			isFlutter = true
		}
	}
	if !isFlutter && (dirExists(filepath.Join(dir, "android")) || dirExists(filepath.Join(dir, "ios"))) {
		isFlutter = true
	}

	compType := ComponentDart
	name := "Dart"
	if isFlutter {
		compType = ComponentFlutter
		name = "Flutter"
	}
	if rel != "" {
		name = fmt.Sprintf("%s (%s)", name, rel)
	}
	return &Component{
		Type:       compType,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func (d *Detector) detectElixir(dir, rel string) *Component {
	hasMix := fileExists(filepath.Join(dir, "mix.exs"))
	hasRebar := fileExists(filepath.Join(dir, "rebar.config"))
	if !hasMix && !hasRebar {
		return nil
	}
	name := "Elixir"
	if hasRebar && !hasMix {
		name = "Erlang"
	}
	if rel != "" {
		name = fmt.Sprintf("%s (%s)", name, rel)
	}
	return &Component{
		Type:       ComponentElixir,
		Name:       name,
		Path:       rel,
		Confidence: "high",
	}
}

func fileExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && !fi.IsDir()
}

func dirExists(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func extractGradleVersion(propsPath string) string {
	f, err := os.Open(propsPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	re := regexp.MustCompile(`gradle-([0-9]+\.[0-9]+(?:\.[0-9]+)?)-`)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.Contains(line, "distributionUrl") {
			matches := re.FindStringSubmatch(line)
			if len(matches) > 1 {
				return matches[1]
			}
		}
	}
	return ""
}

func extractCompileSdk(buildFile string) string {
	data, err := os.ReadFile(buildFile)
	if err != nil {
		return ""
	}
	re := regexp.MustCompile(`compileSdk(?:Version)?\s*=?\s*([0-9]+)`)
	matches := re.FindStringSubmatch(string(data))
	if len(matches) > 1 {
		return matches[1]
	}
	return "35"
}

func formatComponentName(compType, relPath string) string {
	var title string
	switch strings.ToLower(compType) {
	case "android":
		title = "Android"
	case "zephyr":
		title = "Zephyr"
	case "rust":
		title = "Rust"
	case "go":
		title = "Go"
	case "node":
		title = "Node.js"
	case "python":
		title = "Python"
	case "cmake":
		title = "CMake"
	case "cpp":
		title = "C++"
	case "c":
		title = "C"
	case "java":
		title = "Java"
	case "swift":
		title = "Swift"
	case "ruby":
		title = "Ruby"
	case "php":
		title = "PHP"
	case "zig":
		title = "Zig"
	case "dotnet":
		title = ".NET"
	case "flutter":
		title = "Flutter"
	case "dart":
		title = "Dart"
	case "elixir":
		title = "Elixir"
	default:
		if len(compType) > 0 {
			title = strings.ToUpper(compType[:1]) + compType[1:]
		} else {
			title = "Generic"
		}
	}
	if relPath != "" {
		return fmt.Sprintf("%s (%s)", title, relPath)
	}
	return title
}
