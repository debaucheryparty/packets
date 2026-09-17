package android

import (
	"fmt"
	"strings"
)

func GenerateSetupScript(avdName string, apiLevel int) string {
	if avdName == "" {
		avdName = "Pixel_8_API_34"
	}
	if apiLevel <= 0 {
		apiLevel = 34
	}

	tmpl := `#!/usr/bin/env bash
set -e

export ANDROID_HOME="${HOME}/android-sdk"
mkdir -p "${ANDROID_HOME}/cmdline-tools"

if command -v apt-get >/dev/null 2>&1; then
    sudo apt-get update -y
    sudo apt-get install -y curl unzip openjdk-17-jdk-headless libpulse0 libx11-xcb1 libxcomposite1 libxcursor1 libxi6 libdrm2 libxkbfile1
fi

if [ -e /dev/kvm ]; then
    sudo chmod 666 /dev/kvm || true
fi

if [ ! -f "${ANDROID_HOME}/cmdline-tools/latest/bin/sdkmanager" ]; then
    curl -fsSL https://dl.google.com/android/repository/commandlinetools-linux-11076708_latest.zip -o /tmp/cmdline-tools.zip
    unzip -q /tmp/cmdline-tools.zip -d /tmp/cmdline-unpacked
    mkdir -p "${ANDROID_HOME}/cmdline-tools/latest"
    cp -r /tmp/cmdline-unpacked/cmdline-tools/* "${ANDROID_HOME}/cmdline-tools/latest/"
    rm -f /tmp/cmdline-tools.zip
    rm -rf /tmp/cmdline-unpacked
fi

export PATH="${ANDROID_HOME}/cmdline-tools/latest/bin:${ANDROID_HOME}/platform-tools:${ANDROID_HOME}/emulator:${PATH}"

link_binaries() {
    for bin in sdkmanager avdmanager adb emulator; do
        src=$(find "${ANDROID_HOME}" -name "$bin" -type f -perm -111 2>/dev/null | head -n 1)
        if [ -n "$src" ] && command -v sudo >/dev/null 2>&1; then
            sudo ln -sf "$src" "/usr/local/bin/$bin" || true
        fi
    done
}

link_binaries
yes | sdkmanager --licenses > /dev/null 2>&1 || true

sdkmanager "platform-tools" "emulator" "platforms;android-{{API_LEVEL}}" "system-images;android-{{API_LEVEL}};google_apis;x86_64"
link_binaries

if ! avdmanager list avd -c | grep -q "^{{AVD_NAME}}$"; then
    echo "no" | avdmanager create avd -n "{{AVD_NAME}}" -k "system-images;android-{{API_LEVEL}};google_apis;x86_64" --force
fi

avdmanager list avd
`
	script := strings.ReplaceAll(tmpl, "{{API_LEVEL}}", fmt.Sprintf("%d", apiLevel))
	script = strings.ReplaceAll(script, "{{AVD_NAME}}", avdName)
	return script
}
