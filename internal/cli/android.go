package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/debaucheryparty/packets/internal/android"
	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/workspace"
	"github.com/debaucheryparty/packets/pkg/apitypes"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
)

func NewAndroidCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "android",
		Short: "Remote Android development",
	}
	cmd.AddCommand(
		newAndroidBuildCommand(cfg, logger),
		newAndroidDevicesCommand(cfg, logger),
		newAndroidInstallCommand(cfg, logger),
		newAndroidRunCommand(cfg, logger),
		newAndroidShellCommand(cfg, logger),
		newAndroidLogcatCommand(cfg, logger),
		newAndroidTestCommand(cfg, logger),
		newAndroidEmulatorCommand(cfg, logger),
		newAndroidDevCommand(cfg, logger),
		newAndroidNodeCheckCommand(logger),
		newAndroidConnectCommand(cfg, logger),
	)
	return cmd
}

func newAndroidBuildCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build [dir]",
		Short: "Run a remote Gradle build and return the APK",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			fmt.Println("Detecting Android project...")
			proj, err := android.DetectProject(dir)
			if err != nil {
				return fmt.Errorf("project detection: %w", err)
			}
			if !proj.IsValid() {
				return fmt.Errorf("no Android project found in %s\n"+
					"  Required: gradlew + app/ directory", dir)
			}
			fmt.Printf("✓ Android project detected (confidence: %s)\n\n", proj.Confidence())

			variant, _ := cmd.Flags().GetString("variant")
			waitFlag, _ := cmd.Flags().GetBool("wait")
			providerFlag, _ := cmd.Flags().GetString("provider")
			forceFlag, _ := cmd.Flags().GetBool("force")
			benchmarkFlag, _ := cmd.Flags().GetBool("benchmark")

			totalStart := time.Now()
			gradleTask := variantToTask(variant)
			artifactGlob := variantToArtifact(variant)

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return fmt.Errorf("connect to packetsd: %w", err)
			}
			defer conn.Close()

			uploadStart := time.Now()
			fmt.Println("Uploading project to remote node...")
			snapshotRef, err := workspace.UploadWorkspace(ctx, conn, proj.Root, forceFlag)
			if err != nil {
				return fmt.Errorf("workspace upload: %w", err)
			}
			uploadDur := time.Since(uploadStart)
			fmt.Printf("✓ Workspace uploaded (ref: %s, duration: %s)\n\n", snapshotRef, uploadDur.Round(time.Millisecond))

			_ = providerFlag

			fmt.Println("Collecting build environment fingerprint...")
			cacheInputs, err := android.CollectCacheInputs(ctx, proj.Root, variant, snapshotRef)
			if err != nil {
				cacheInputs = &android.AndroidCacheInputs{SourceHash: snapshotRef, Variant: variant}
			}
			cacheKey := android.CacheKey(*cacheInputs)

			client := pb.NewSchedulerClient(conn)
			resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
				CacheKey:      cacheKey,
				Toolchain:     string(apitypes.ToolchainAndroid),
				SnapshotRef:   snapshotRef,
				Runner:        string(apitypes.RunnerDocker),
				SourceMode:    string(apitypes.SourceModeWorkspace),
				CommandArgs:   []string{gradleTask},
				ArtifactPaths: []string{artifactGlob},
			})
			if err != nil {
				return fmt.Errorf("submit job: %w", err)
			}

			fmt.Printf("Running Gradle (%s)...\n", gradleTask)
			if resp.CacheHit {
				fmt.Println("✓ Cache hit — reusing existing APK")
			}

			if waitFlag {
				buildStart := time.Now()
				err := pollJobStatus(ctx, cfg, client, resp.JobId, proj.Root, logger)
				if err != nil {
					return err
				}
				buildDur := time.Since(buildStart)
				totalDur := time.Since(totalStart)
				if benchmarkFlag {
					fmt.Println("\n=== Benchmark Report ===")
					fmt.Printf("Workspace Sync:  %s\n", uploadDur.Round(time.Millisecond))
					fmt.Printf("Remote Gradle:   %s\n", buildDur.Round(time.Millisecond))
					fmt.Printf("Total Duration:  %s\n", totalDur.Round(time.Millisecond))
					fmt.Println("========================")
				}
				return nil
			}

			fmt.Printf("\nJob submitted: %s\n", resp.JobId)
			fmt.Printf("Monitor with: packets status %s\n", resp.JobId)
			return nil
		},
	}
	cmd.Flags().String("variant", "debug", "Build variant: debug or release")
	cmd.Flags().Bool("wait", true, "Wait for build and download APK")
	cmd.Flags().String("provider", "", "Named provider from config")
	cmd.Flags().Bool("force", false, "Re-upload entire workspace")
	cmd.Flags().Bool("benchmark", false, "Measure build timing (Phase 9)")
	return cmd
}

func variantToTask(v string) string {
	if v == "release" {
		return "assembleRelease"
	}
	return "assembleDebug"
}

func variantToArtifact(v string) string {
	if v == "release" {
		return "app/build/outputs/apk/release/*.apk"
	}
	return "app/build/outputs/apk/debug/*.apk"
}

func newAndroidDevicesCommand(_ *config.Config, logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "devices",
		Short: "List Android devices/emulators on the remote node",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = logger
			adb := android.NewExecADBClient()
			devices, err := adb.Devices(cmd.Context())
			if err != nil {
				return fmt.Errorf("adb devices: %w", err)
			}
			if len(devices) == 0 {
				fmt.Println("No devices connected.")
				return nil
			}
			for _, d := range devices {
				fmt.Printf("%-25s  %s\n", d.Serial, d.State)
			}
			return nil
		},
	}
}

func newAndroidInstallCommand(_ *config.Config, _ *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "install <apk>",
		Short: "Install an APK on the remote device",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			apk := args[0]
			adb := android.NewExecADBClient()
			fmt.Printf("Installing %s...\n", apk)
			if err := adb.Install(cmd.Context(), serial, apk); err != nil {
				return fmt.Errorf("install: %w", err)
			}
			fmt.Println("✓ Installed")
			return nil
		},
	}
	cmd.Flags().String("serial", "", "Device serial (empty = first device)")
	return cmd
}

func newAndroidRunCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run [dir]",
		Short: "Build, install, and launch the app on the remote emulator",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			serial, _ := cmd.Flags().GetString("serial")
			pkg, _ := cmd.Flags().GetString("package")
			activity, _ := cmd.Flags().GetString("activity")

			buildCmd := newAndroidBuildCommand(cfg, logger)
			buildCmd.SetArgs([]string{dir, "--wait=true"})
			if err := buildCmd.ExecuteContext(ctx); err != nil {
				return fmt.Errorf("android build: %w", err)
			}

			apk, err := findAPK(dir, "debug")
			if err != nil {
				return fmt.Errorf("find APK: %w", err)
			}

			adb := android.NewExecADBClient()

			fmt.Printf("\nInstalling %s...\n", apk)
			if err := adb.Install(ctx, serial, apk); err != nil {
				return fmt.Errorf("install: %w", err)
			}
			fmt.Println("✓ Installed")

			if pkg == "" {
				fmt.Println("\nNote: use --package and --activity to specify the entry point.")
				fmt.Println("Example: packets android run . --package=com.example.app --activity=.MainActivity")
				return nil
			}
			target := pkg + "/" + activity
			fmt.Printf("Launching %s...\n", target)
			out, err := adb.Shell(ctx, serial, "am", "start", "-n", target)
			if err != nil {
				return fmt.Errorf("launch %s: %w\n%s", target, err, out)
			}
			fmt.Println("✓ Application launched")
			return nil
		},
	}
	cmd.Flags().String("serial", "", "Device serial")
	cmd.Flags().String("package", "", "Android package name (e.g. com.example.app)")
	cmd.Flags().String("activity", ".MainActivity", "Activity to launch")
	return cmd
}

func findAPK(dir, variant string) (string, error) {
	pattern := fmt.Sprintf("%s/app/build/outputs/apk/%s/*.apk", dir, variant)
	matches, err := filepath.Glob(pattern)
	if err != nil || len(matches) == 0 {
		return "", fmt.Errorf("no APK found at %s", pattern)
	}
	return matches[0], nil
}

func newAndroidShellCommand(_ *config.Config, _ *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "shell",
		Short: "Open an interactive ADB shell inside the remote Android device",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			adb := android.NewExecADBClient()
			fmt.Printf("Connected to: %s\n\n", serial)
			return adb.ShellStream(cmd.Context(), serial, os.Stdout)
		},
	}
	cmd.Flags().String("serial", "", "Device serial")
	return cmd
}

func newAndroidLogcatCommand(_ *config.Config, _ *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logcat",
		Short: "Stream logcat output from the remote Android device",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			adb := android.NewExecADBClient()
			return adb.Logcat(cmd.Context(), serial, os.Stdout)
		},
	}
	cmd.Flags().String("serial", "", "Device serial")
	return cmd
}

func newAndroidTestCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "test [dir]",
		Short: "Run instrumentation tests on the remote emulator",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			proj, err := android.DetectProject(dir)
			if err != nil {
				return fmt.Errorf("project detection: %w", err)
			}
			if !proj.IsValid() {
				return fmt.Errorf("no Android project found in %s", dir)
			}

			conn, err := DialScheduler(ctx, cfg)
			if err != nil {
				return err
			}
			defer conn.Close()

			snapshotRef, err := workspace.UploadWorkspace(ctx, conn, proj.Root, false)
			if err != nil {
				return fmt.Errorf("workspace upload: %w", err)
			}

			cacheKey := fmt.Sprintf("android-test:%s", snapshotRef)
			client := pb.NewSchedulerClient(conn)
			resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
				CacheKey:    cacheKey,
				Toolchain:   string(apitypes.ToolchainAndroid),
				SnapshotRef: snapshotRef,
				Runner:      string(apitypes.RunnerDocker),
				SourceMode:  string(apitypes.SourceModeWorkspace),
				CommandArgs: []string{"connectedAndroidTest"},
			})
			if err != nil {
				return fmt.Errorf("submit job: %w", err)
			}

			fmt.Println("Running connectedAndroidTest...")
			return pollJobStatus(ctx, cfg, client, resp.JobId, proj.Root, logger)
		},
	}
	return cmd
}

func newAndroidEmulatorCommand(_ *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "emulator",
		Short: "Manage the remote Android emulator",
	}

	adb := android.NewExecADBClient()
	mgr := android.NewAVDEmulatorManager(logger, adb)

	startCmd := &cobra.Command{
		Use:   "start",
		Short: "Start the remote Android emulator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			avd, _ := cmd.Flags().GetString("avd")
			cores, _ := cmd.Flags().GetInt("cores")
			ram, _ := cmd.Flags().GetInt("ram")
			gpu, _ := cmd.Flags().GetString("gpu")
			snapshot, _ := cmd.Flags().GetString("snapshot")
			noSnapshotLoad, _ := cmd.Flags().GetBool("no-snapshot-load")

			fmt.Printf("Starting emulator %s...\n", avd)
			serial, err := mgr.Start(ctx, avd, android.EmulatorStartOpts{
				Cores:          cores,
				RAMMb:          ram,
				GPU:            gpu,
				NoWindow:       true,
				NoAudio:        true,
				Snapshot:       snapshot,
				NoSnapshotLoad: noSnapshotLoad,
			})
			if err != nil {
				return fmt.Errorf("emulator start: %w", err)
			}

			fmt.Printf("✓ Emulator started (serial: %s)\n", serial)
			fmt.Println("Waiting for Android to boot...")
			if err := mgr.WaitForBoot(ctx, serial, 5*time.Minute); err != nil {
				return fmt.Errorf("boot timeout: %w", err)
			}
			fmt.Println("✓ Boot complete")
			return nil
		},
	}
	startCmd.Flags().String("avd", "Pixel_8_API_35", "AVD name to start")
	startCmd.Flags().Int("cores", 4, "Number of CPU cores")
	startCmd.Flags().Int("ram", 4096, "RAM in MB")
	startCmd.Flags().String("gpu", "auto", "GPU mode: auto, host, swiftshader_indirect, off")
	startCmd.Flags().String("snapshot", "", "Name of snapshot to boot from")
	startCmd.Flags().Bool("no-snapshot-load", false, "Force cold boot without restoring snapshot")
	stopCmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the remote Android emulator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			fmt.Printf("Stopping %s...\n", serial)
			if err := mgr.Stop(cmd.Context(), serial); err != nil {
				return fmt.Errorf("emulator stop: %w", err)
			}
			fmt.Println("✓ Emulator stopped")
			return nil
		},
	}
	stopCmd.Flags().String("serial", "emulator-5554", "Device serial to stop")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show status of running emulators",
		RunE: func(cmd *cobra.Command, _ []string) error {
			infos, err := mgr.List(cmd.Context())
			if err != nil {
				return fmt.Errorf("list emulators: %w", err)
			}
			if len(infos) == 0 {
				fmt.Println("No running emulators.")
				return nil
			}
			for _, e := range infos {
				fmt.Printf("%-20s  %s\n", e.Serial, e.State)
			}
			return nil
		},
	}

	snapshotCmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Manage emulator snapshots for fast booting",
	}

	snapSaveCmd := &cobra.Command{
		Use:   "save <name>",
		Short: "Save current emulator state as a named snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			name := args[0]
			fmt.Printf("Saving snapshot %q on %s...\n", name, serial)
			if err := mgr.SaveSnapshot(cmd.Context(), serial, name); err != nil {
				return err
			}
			fmt.Printf("✓ Snapshot %q saved\n", name)
			return nil
		},
	}
	snapSaveCmd.Flags().String("serial", "emulator-5554", "Device serial")

	snapLoadCmd := &cobra.Command{
		Use:   "load <name>",
		Short: "Restore emulator state from a named snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			name := args[0]
			fmt.Printf("Restoring snapshot %q on %s...\n", name, serial)
			if err := mgr.LoadSnapshot(cmd.Context(), serial, name); err != nil {
				return err
			}
			fmt.Printf("✓ Snapshot %q restored\n", name)
			return nil
		},
	}
	snapLoadCmd.Flags().String("serial", "emulator-5554", "Device serial")

	snapListCmd := &cobra.Command{
		Use:   "list",
		Short: "List snapshots on the running emulator",
		RunE: func(cmd *cobra.Command, _ []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			snapshots, err := mgr.ListSnapshots(cmd.Context(), serial)
			if err != nil {
				return err
			}
			if len(snapshots) == 0 {
				fmt.Println("No snapshots found.")
				return nil
			}
			fmt.Printf("Snapshots on %s:\n", serial)
			for _, s := range snapshots {
				fmt.Printf("  • %s\n", s)
			}
			return nil
		},
	}
	snapListCmd.Flags().String("serial", "emulator-5554", "Device serial")

	snapDeleteCmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			serial, _ := cmd.Flags().GetString("serial")
			name := args[0]
			fmt.Printf("Deleting snapshot %q on %s...\n", name, serial)
			if err := mgr.DeleteSnapshot(cmd.Context(), serial, name); err != nil {
				return err
			}
			fmt.Printf("✓ Snapshot %q deleted\n", name)
			return nil
		},
	}
	snapDeleteCmd.Flags().String("serial", "emulator-5554", "Device serial")

	snapshotCmd.AddCommand(snapSaveCmd, snapLoadCmd, snapListCmd, snapDeleteCmd)

	cmd.AddCommand(startCmd, stopCmd, statusCmd, snapshotCmd)
	return cmd
}

func newAndroidDevCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dev [dir]",
		Short: "Full remote Android dev loop: build, install, run, display, and watch for changes",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			dir := "."
			if len(args) > 0 {
				dir = args[0]
			}

			avd, _ := cmd.Flags().GetString("avd")
			serial, _ := cmd.Flags().GetString("serial")
			pkg, _ := cmd.Flags().GetString("package")
			activity, _ := cmd.Flags().GetString("activity")
			variant, _ := cmd.Flags().GetString("variant")
			adbHost, _ := cmd.Flags().GetString("adb-host")
			adbPort, _ := cmd.Flags().GetInt("adb-port")
			noDisplay, _ := cmd.Flags().GetBool("no-display")
			qualityStr, _ := cmd.Flags().GetString("quality")
			maxSizeFlag, _ := cmd.Flags().GetInt("max-size")
			bitrateFlag, _ := cmd.Flags().GetInt("bitrate")
			maxFPSFlag, _ := cmd.Flags().GetInt("max-fps")
			snapshot, _ := cmd.Flags().GetString("snapshot")
			noSnapshotLoad, _ := cmd.Flags().GetBool("no-snapshot-load")

			fmt.Println("Android project detected        ", checkMark(true))
			proj, err := android.DetectProject(dir)
			if err != nil || !proj.IsValid() {
				return fmt.Errorf("no Android project found in %s (need gradlew + app/)", dir)
			}
			fmt.Printf("✓ Android project detected (confidence: %s)\n", proj.Confidence())

			adbClient := android.NewExecADBClient()
			emulatorMgr := android.NewAVDEmulatorManager(logger, adbClient)

			if serial == "" {
				fmt.Printf("\nStarting emulator %s...\n", avd)
				serial, err = emulatorMgr.Start(ctx, avd, android.EmulatorStartOpts{
					NoWindow:       true,
					NoAudio:        true,
					GPU:            "auto",
					Snapshot:       snapshot,
					NoSnapshotLoad: noSnapshotLoad,
				})
				if err != nil {
					return fmt.Errorf("emulator start: %w\nSuggestion: run `packets android node check` to verify KVM and SDK are available", err)
				}
				fmt.Printf("✓ Emulator started (%s)\n", serial)

				fmt.Println("Waiting for Android to boot...")
				if err := emulatorMgr.WaitForBoot(ctx, serial, 5*time.Minute); err != nil {
					return fmt.Errorf("boot timeout: %w", err)
				}
				fmt.Println("✓ Boot complete")
			}

			if err := runBuildInstallLaunch(ctx, cfg, logger, proj.Root, variant, serial, pkg, activity); err != nil {
				return err
			}
			if !noDisplay && adbHost != "" {
				display := android.NewScrcpyDisplayBackend(logger)
				defMaxSize, defBitrate, defFPS := android.QualityToOpts(android.DisplayQuality(qualityStr))
				maxSize := defMaxSize
				if maxSizeFlag > 0 {
					maxSize = maxSizeFlag
				}
				bitrate := defBitrate
				if bitrateFlag > 0 {
					bitrate = bitrateFlag
				}
				maxFPS := defFPS
				if maxFPSFlag > 0 {
					maxFPS = maxFPSFlag
				}

				fmt.Printf("\nStarting remote display via scrcpy (%s:%d, quality: %s)...\n", adbHost, adbPort, qualityStr)
				go func() {
					if err := display.Start(ctx, android.DisplayOpts{
						ADBHost:    adbHost,
						ADBPort:    adbPort,
						Serial:     serial,
						MaxSizePx:  maxSize,
						BitrateBps: bitrate,
						MaxFPS:     maxFPS,
						Stderr:     os.Stderr,
					}); err != nil && ctx.Err() == nil {
						logger.Warn("scrcpy exited", slog.String("err", err.Error()))
					}
				}()
				fmt.Println("✓ Remote display connected (scrcpy window should open)")
			} else if !noDisplay && adbHost == "" {
				fmt.Println("\nNote: pass --adb-host=<tailscale-ip> to start the remote display.")
			}

			go func() {
				_ = adbClient.Logcat(ctx, serial, os.Stdout)
			}()
			fmt.Println("\nWatching for source changes... (Ctrl-C to stop)")
			watcher := android.NewWatcher(proj.Root, 2*time.Second, nil)
			go watcher.Start(ctx)

			for {
				select {
				case <-ctx.Done():
					fmt.Println("\nStopping dev session.")
					return nil
				case <-watcher.Errors:
				case changes := <-watcher.Changes:
					if !hasSourceChanges(changes) {
						continue
					}
					fmt.Printf("\n📦 %d file(s) changed — rebuilding...\n", len(changes))
					if err := runBuildInstallLaunch(ctx, cfg, logger, proj.Root, variant, serial, pkg, activity); err != nil {
						logger.Error("rebuild failed", slog.String("err", err.Error()))
						fmt.Println("⚠ Rebuild failed. Watching for next change...")
					}
				}
			}
		},
	}

	cmd.Flags().String("avd", "Pixel_8_API_35", "AVD name to start")
	cmd.Flags().String("serial", "", "Reuse an already-running emulator serial instead of starting a new one")
	cmd.Flags().String("package", "", "Android package name (e.g. com.example.app)")
	cmd.Flags().String("activity", ".MainActivity", "Activity to launch")
	cmd.Flags().String("variant", "debug", "Build variant: debug or release")
	cmd.Flags().String("adb-host", "", "Tailscale IP of the remote node for scrcpy display")
	cmd.Flags().Int("adb-port", 5037, "ADB server port on the remote node")
	cmd.Flags().Bool("no-display", false, "Skip starting the scrcpy display window")
	cmd.Flags().String("quality", "medium", "Display quality preset: low, medium, or high (Section 63)")
	cmd.Flags().Int("max-size", 0, "Custom display maximum dimension in pixels (overrides quality preset)")
	cmd.Flags().Int("bitrate", 0, "Custom display video bitrate in bps (overrides quality preset)")
	cmd.Flags().Int("max-fps", 0, "Custom display maximum FPS (overrides quality preset)")
	cmd.Flags().String("snapshot", "", "Name of snapshot to restore on emulator boot")
	cmd.Flags().Bool("no-snapshot-load", false, "Force cold boot without restoring snapshot")
	return cmd
}

func newAndroidConnectCommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "connect",
		Short: "Connect local ADB to the remote Android emulator for Android Studio integration",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = cfg
			_ = logger
			ctx := cmd.Context()
			host, _ := cmd.Flags().GetString("host")
			port, _ := cmd.Flags().GetInt("port")

			if host == "" {
				host = os.Getenv("ORACLE_VM_TAILSCALE_HOSTNAME")
				if host == "" {
					return fmt.Errorf("missing remote host: specify --host=<tailscale-ip> or set ORACLE_VM_TAILSCALE_HOSTNAME")
				}
			}

			target := fmt.Sprintf("%s:%d", host, port)
			fmt.Printf("Connecting local ADB to remote emulator at %s...\n", target)

			out, err := exec.CommandContext(ctx, "adb", "connect", target).CombinedOutput()
			if err != nil {
				return fmt.Errorf("adb connect %s: %w\n%s", target, err, string(out))
			}
			fmt.Println(strings.TrimSpace(string(out)))

			devicesOut, err := exec.CommandContext(ctx, "adb", "devices").CombinedOutput()
			if err == nil {
				fmt.Printf("\nActive ADB devices:\n%s\n", string(devicesOut))
			}
			fmt.Println("✓ Android Studio is now ready to detect and target this remote emulator.")
			return nil
		},
	}
	cmd.Flags().String("host", "", "Tailscale hostname or IP of the remote Android node")
	cmd.Flags().Int("port", 5555, "ADB daemon port on the remote device/node (default 5555)")
	return cmd
}

func runBuildInstallLaunch(
	ctx context.Context,
	cfg *config.Config,
	logger *slog.Logger,
	projectRoot, variant, serial, pkg, activity string,
) error {
	conn, err := DialScheduler(ctx, cfg)
	if err != nil {
		return fmt.Errorf("connect to packetsd: %w", err)
	}
	defer conn.Close()

	fmt.Println("Uploading workspace...")
	snapshotRef, err := workspace.UploadWorkspace(ctx, conn, projectRoot, false)
	if err != nil {
		return fmt.Errorf("workspace upload: %w", err)
	}

	cacheInputs, _ := android.CollectCacheInputs(ctx, projectRoot, variant, snapshotRef)
	if cacheInputs == nil {
		cacheInputs = &android.AndroidCacheInputs{SourceHash: snapshotRef, Variant: variant}
	}
	cacheKey := android.CacheKey(*cacheInputs)
	gradleTask := variantToTask(variant)
	artifactGlob := variantToArtifact(variant)

	client := pb.NewSchedulerClient(conn)
	resp, err := client.SubmitJob(ctx, &pb.SubmitJobRequest{
		CacheKey:      cacheKey,
		Toolchain:     string(apitypes.ToolchainAndroid),
		SnapshotRef:   snapshotRef,
		Runner:        string(apitypes.RunnerDocker),
		SourceMode:    string(apitypes.SourceModeWorkspace),
		CommandArgs:   []string{gradleTask},
		ArtifactPaths: []string{artifactGlob},
	})
	if err != nil {
		return fmt.Errorf("submit job: %w", err)
	}

	if resp.CacheHit {
		fmt.Println("✓ Cache hit — reusing APK")
	} else {
		fmt.Printf("Running Gradle (%s)...\n", gradleTask)
		if err := pollJobStatus(ctx, cfg, client, resp.JobId, projectRoot, logger); err != nil {
			return fmt.Errorf("Gradle build failed.\n\nJob: %s\nVariant: %s\n\nRemote output: check with `packets logs %s`\n\nError: %w",
				resp.JobId, variant, resp.JobId, err)
		}
		fmt.Println("✓ Build complete")
	}

	apk, err := findAPK(projectRoot, variant)
	if err != nil {
		return fmt.Errorf("no APK found after build: %w", err)
	}

	adb := android.NewExecADBClient()
	fmt.Printf("Installing %s...\n", filepath.Base(apk))
	if err := adb.Install(ctx, serial, apk); err != nil {
		return fmt.Errorf("adb install: %w", err)
	}
	fmt.Println("✓ Installed")

	if pkg != "" {
		target := pkg + "/" + activity
		fmt.Printf("Launching %s...\n", target)
		_, _ = adb.Shell(ctx, serial, "am", "force-stop", pkg)
		out, err := adb.Shell(ctx, serial, "am", "start", "-n", target)
		if err != nil {
			return fmt.Errorf("launch %s: %w\n%s", target, err, out)
		}
		fmt.Println("✓ Application launched")
	}
	return nil
}

func hasSourceChanges(changes []android.FileChangeEvent) bool {
	sourceExts := map[string]bool{
		".kt": true, ".java": true, ".xml": true,
		".gradle": true, ".kts": true, ".properties": true,
		".json": true, ".yaml": true, ".yml": true,
	}
	for _, c := range changes {
		ext := filepath.Ext(c.Path)
		if sourceExts[ext] {
			return true
		}
	}
	return false
}

func checkMark(ok bool) string {
	if ok {
		return "✓"
	}
	return "✗"
}

func newAndroidNodeCheckCommand(logger *slog.Logger) *cobra.Command {
	return &cobra.Command{
		Use:   "node-check",
		Short: "Check whether this node has all Android development prerequisites",
		RunE: func(cmd *cobra.Command, _ []string) error {
			_ = logger
			ctx := cmd.Context()
			fmt.Println("Checking Android node capabilities...")

			caps, err := android.CheckCapabilities(ctx)
			if err != nil {
				return fmt.Errorf("capability check: %w", err)
			}

			printCap := func(label string, ok bool, detail string) {
				mark := "✓"
				if !ok {
					mark = "✗"
				}
				if detail != "" {
					fmt.Printf("  %s  %-20s  %s\n", mark, label, detail)
				} else {
					fmt.Printf("  %s  %s\n", mark, label)
				}
			}

			fmt.Printf("OS:           %s\n", caps.OS)
			fmt.Printf("Architecture: %s\n\n", caps.Arch)

			printCap("JDK", caps.JDKInstalled, caps.JDKVersion)
			printCap("Android SDK", caps.SDKInstalled, caps.SDKRoot)
			printCap("ADB", caps.ADBInstalled, caps.ADBVersion)
			printCap("Emulator", caps.EmulatorInstalled, caps.EmulatorVersion)
			printCap("Build tools", caps.BuildTools, "")
			printCap("scrcpy", caps.ScrCpyInstalled, "")
			printCap("KVM", caps.KVMAvailable, "")

			fmt.Printf("\nStatus: %s\n", caps.Status())
			if reason := caps.DegradedReason(); reason != "" {
				fmt.Printf("Reason: %s\n", reason)
			}
			return nil
		},
	}
}

var _ io.Writer = os.Stdout
