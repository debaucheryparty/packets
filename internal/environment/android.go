package environment

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type AndroidResolver struct{}

func NewAndroidResolver() *AndroidResolver {
	return &AndroidResolver{}
}

func (a *AndroidResolver) Check(ctx context.Context, comp Component, remoteWorker bool) ([]ToolchainRequirement, error) {
	compileSdk := comp.Metadata["compile_sdk"]
	if compileSdk == "" {
		compileSdk = "35"
	}

	reqs := []ToolchainRequirement{
		a.checkJDK(),
		a.checkAndroidSDK(),
		a.checkPlatform(compileSdk),
		a.checkBuildTools(compileSdk),
		a.checkGradleWrapper(comp),
	}

	return reqs, nil
}

func (a *AndroidResolver) checkJDK() ToolchainRequirement {
	req := ToolchainRequirement{
		Name:       "JDK 17+",
		CanPrepare: true,
		PrepareCmd: "apt-get update && apt-get install -y openjdk-17-jdk",
	}

	out, err := exec.Command("java", "-version").CombinedOutput()
	if err == nil {
		req.Status = StatusOk
		req.Details = strings.Split(string(out), "\n")[0]
		return req
	}

	req.Status = StatusMissing
	req.Details = "java not found in PATH"
	return req
}

func (a *AndroidResolver) checkAndroidSDK() ToolchainRequirement {
	req := ToolchainRequirement{
		Name:       "Android SDK",
		CanPrepare: true,
		PrepareCmd: "mkdir -p $ANDROID_HOME/cmdline-tools && wget -q https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip && unzip -q commandlinetools-*.zip -d $ANDROID_HOME/cmdline-tools/latest",
	}

	sdkRoot := os.Getenv("ANDROID_HOME")
	if sdkRoot == "" {
		sdkRoot = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdkRoot == "" && runtime.GOOS == "linux" {
		if _, err := os.Stat("/opt/android-sdk"); err == nil {
			sdkRoot = "/opt/android-sdk"
		}
	}

	if sdkRoot != "" {
		if fi, err := os.Stat(sdkRoot); err == nil && fi.IsDir() {
			req.Status = StatusOk
			req.Details = sdkRoot
			return req
		}
	}

	req.Status = StatusMissing
	req.Details = "ANDROID_HOME not set or directory not found"
	return req
}

func (a *AndroidResolver) checkPlatform(version string) ToolchainRequirement {
	req := ToolchainRequirement{
		Name:       fmt.Sprintf("Android Platform %s", version),
		Version:    version,
		CanPrepare: true,
		PrepareCmd: fmt.Sprintf("sdkmanager \"platforms;android-%s\"", version),
	}

	sdkRoot := os.Getenv("ANDROID_HOME")
	if sdkRoot == "" {
		sdkRoot = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdkRoot != "" {
		platformDir := filepath.Join(sdkRoot, "platforms", "android-"+version)
		if fi, err := os.Stat(platformDir); err == nil && fi.IsDir() {
			req.Status = StatusOk
			req.Details = platformDir
			return req
		}
	}

	req.Status = StatusMissing
	req.Details = fmt.Sprintf("platforms;android-%s not installed", version)
	return req
}

func (a *AndroidResolver) checkBuildTools(platformVersion string) ToolchainRequirement {
	buildToolsVersion := platformVersion + ".0.0"
	req := ToolchainRequirement{
		Name:       fmt.Sprintf("Build Tools %s", buildToolsVersion),
		Version:    buildToolsVersion,
		CanPrepare: true,
		PrepareCmd: fmt.Sprintf("sdkmanager \"build-tools;%s\"", buildToolsVersion),
	}

	sdkRoot := os.Getenv("ANDROID_HOME")
	if sdkRoot == "" {
		sdkRoot = os.Getenv("ANDROID_SDK_ROOT")
	}
	if sdkRoot != "" {
		btDir := filepath.Join(sdkRoot, "build-tools", buildToolsVersion)
		if fi, err := os.Stat(btDir); err == nil && fi.IsDir() {
			req.Status = StatusOk
			req.Details = btDir
			return req
		}
	}

	req.Status = StatusMissing
	req.Details = fmt.Sprintf("build-tools;%s not installed", buildToolsVersion)
	return req
}

func (a *AndroidResolver) checkGradleWrapper(comp Component) ToolchainRequirement {
	req := ToolchainRequirement{
		Name: "Gradle Wrapper",
	}

	if comp.Metadata["gradle_wrapper"] == "true" {
		req.Status = StatusWrapper
		ver := comp.Metadata["gradle_version"]
		if ver != "" {
			req.Details = fmt.Sprintf("configured version %s", ver)
		} else {
			req.Details = "gradle-wrapper.properties detected"
		}
		return req
	}

	req.Status = StatusWarning
	req.Details = "gradle wrapper not found; global gradle will be used"
	return req
}
