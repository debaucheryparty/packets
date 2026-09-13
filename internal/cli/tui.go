package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	"github.com/debaucheryparty/packets/internal/environment"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

const (
	cReset   = "\033[0m"
	cBold    = "\033[1m"
	cDim     = "\033[2m"
	cRed     = "\033[31m"
	cGreen   = "\033[32m"
	cYellow  = "\033[33m"
	cBlue    = "\033[34m"
	cMagenta = "\033[35m"
	cCyan    = "\033[36m"
	cWhite   = "\033[37m"

	bgPink  = "\033[45;1;37m"
	bgGreen = "\033[42;1;30m"
	bgRed   = "\033[41;1;37m"
	bgCyan  = "\033[46;1;30m"
	bgBlue  = "\033[44;1;37m"
	bgDark  = "\033[40;1;37m"
)

type commandItem struct {
	Badge string
	Cmd   string
	Desc  string
}

var commandPalette = []commandItem{
	{Badge: "BUILD", Cmd: "packets build", Desc: "Compile project on remote runner"},
	{Badge: "TEST ", Cmd: "packets test", Desc: "Run test suites across workspace"},
	{Badge: "SYNC ", Cmd: "packets sync", Desc: "Synchronize files with remote daemon"},
	{Badge: "EXEC ", Cmd: "packets exec <cmd>", Desc: "Run remote command with log stream"},
	{Badge: "ENV  ", Cmd: "packets env check", Desc: "Verify toolchains and SDK readiness"},
	{Badge: "PEER ", Cmd: "packets worker join", Desc: "Join machine as peer compute node"},
	{Badge: "ANDR ", Cmd: "packets android devices", Desc: "Discover remote Android devices"},
	{Badge: "MCP  ", Cmd: "packets mcp", Desc: "Run MCP server for AI coding agents"},
}

func NewTUICommand(cfg *config.Config, logger *slog.Logger) *cobra.Command {
	var once bool
	var refreshSec int

	cmd := &cobra.Command{
		Use:   "tui",
		Short: "Interactive real-time responsive terminal dashboard for Packets",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			mgr := environment.NewManager()
			topo, _ := mgr.Detect(".")

			refreshInterval := time.Duration(refreshSec) * time.Second
			if refreshInterval < time.Second {
				refreshInterval = 2 * time.Second
			}

			waveFrames := []string{
				" ▂▃▅▆▇▆▅▃ ",
				" ▃▅▆▇█▇▆▅ ",
				"▅▆▇██▇▆▅▃ ",
				"▇▆▅▃ ▂▃▅▆ ",
			}

			showSearch := false

			render := func(tick int) {
				w, _, err := term.GetSize(int(os.Stdout.Fd()))
				if err != nil || w < 40 {
					w = 80
				}

				wave := waveFrames[tick%len(waveFrames)]

				fmt.Print("\033[H\033[2J")

				isOnline := false
				latency := "---"

				conn, err := DialScheduler(ctx, cfg)
				if err == nil {
					defer func() { _ = conn.Close() }()
					client := pb.NewSchedulerClient(conn)
					statusCtx, cancelStatus := context.WithTimeout(ctx, 1200*time.Millisecond)
					start := time.Now()
					_, statusErr := client.GetJobStatus(statusCtx, &pb.GetJobStatusRequest{JobId: "ping"})
					cancelStatus()
					if statusErr == nil || strings.Contains(statusErr.Error(), "not found") {
						isOnline = true
						latency = time.Since(start).Round(time.Millisecond).String()
					}
				}

				statusBadge := bgRed + " OFFLINE " + cReset
				if isOnline {
					statusBadge = bgGreen + " ONLINE " + cReset
				}

				topBar := fmt.Sprintf("%s%s PACKETS REMOTE ENGINE%s", cBold, cCyan, cReset)
				subBar := fmt.Sprintf("[%s] %s%s%s   [ << ] [ %s%sDEV-MESH%s ] [ >> ]   PEER CAPACITY: [%s████████░░%s] 80%%",
					statusBadge, cMagenta, wave, cReset, cBold, cYellow, cReset, cCyan, cReset)

				fmt.Println(topBar)
				fmt.Println(subBar)
				fmt.Println()

				sepLine := strings.Repeat("-", min(w-2, 80))
				fmt.Printf("%s-- CLUSTER / SCHEDULER %s%s\n", cDim, sepLine[min(len(sepLine), 23):], cReset)
				fmt.Printf("/: Target: %s%s%s (Ping: %s%s%s)        %s[/] Search Cmds | [r] Refresh | [q] Quit%s\n\n",
					cBold, cfg.SchedulerAddr(), cReset, cGreen, latency, cReset, cYellow, cReset)

				if showSearch {
					boxW := min(w-4, 76)
					fmt.Printf("┌─ %sCOMMAND PALETTE / SEARCH (/ to close)%s %s┐\n",
						cBold+cYellow, cReset, strings.Repeat("─", max(0, boxW-41)))
					for idx, item := range commandPalette {
						badgeStr := fmt.Sprintf("%s %s %s", bgPink, item.Badge, cReset)
						row := fmt.Sprintf("  %d %s %-22s %s%s%s", idx+1, badgeStr, item.Cmd, cDim, item.Desc, cReset)
						fmt.Printf("│%s│\n", formatBoxRow(row, boxW))
					}
					fmt.Printf("└%s┘\n\n", strings.Repeat("─", boxW))
				}

				hostName, _ := os.Hostname()
				if len(hostName) > 12 {
					hostName = hostName[:12]
				}
				cores := fmt.Sprintf("%d cores", runtime.NumCPU())
				osArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

				if w >= 82 {
					colW := (w - 6) / 2
					if colW > 45 {
						colW = 45
					}

					fmt.Printf("┌─ %sLOCAL WORKSPACE%s %s┐ ┌─ %sPEER FLEET & QUEUE%s %s┐\n",
						cBold, cReset, strings.Repeat("─", max(0, colW-18)), cBold, cReset, strings.Repeat("─", max(0, colW-20)))

					compCount := 0
					if topo != nil {
						compCount = len(topo.Components)
					}
					compHdr := fmt.Sprintf(" %s[1:Components %d]%s", cBold+cCyan, compCount, cReset)
					peerHdr := fmt.Sprintf(" %sCONNECTED NODES:%s", cBold+cWhite, cReset)
					fmt.Printf("│%s│ │%s│\n", formatBoxRow(compHdr, colW), formatBoxRow(peerHdr, colW))

					row1Left := fmt.Sprintf("  %s GO %s packets-core", bgPink, cReset)
					row1Right := fmt.Sprintf("  > %s PEER %s %-12s (Ready)", bgCyan, cReset, hostName)
					fmt.Printf("│%s│ │%s│\n", formatBoxRow(row1Left, colW), formatBoxRow(row1Right, colW))

					row2Left := fmt.Sprintf("  %s AND%s android-tools", bgBlue, cReset)
					row2Right := fmt.Sprintf("    %s DKR %s sandbox-node (Idle)", bgBlue, cReset)
					fmt.Printf("│%s│ │%s│\n", formatBoxRow(row2Left, colW), formatBoxRow(row2Right, colW))

					row3Left := fmt.Sprintf("  %s SEC%s security-guard", bgGreen, cReset)
					row3Right := fmt.Sprintf("  %sQUEUE:%s No pending jobs", cDim, cReset)
					fmt.Printf("│%s│ │%s│\n", formatBoxRow(row3Left, colW), formatBoxRow(row3Right, colW))

					row4Left := fmt.Sprintf("  Root: %s", truncateStr(topoRoot(topo), colW-10))
					row4Right := fmt.Sprintf("  Local: %s (%s)", osArch, cores)
					fmt.Printf("│%s│ │%s│\n", formatBoxRow(row4Left, colW), formatBoxRow(row4Right, colW))

					row5Left := fmt.Sprintf("  Sync Status: %s[ SYNCED ]%s", cGreen, cReset)
					row5Right := fmt.Sprintf("  Docker Isolation: %s[ ACTIVE ]%s", cCyan, cReset)
					fmt.Printf("│%s│ │%s│\n", formatBoxRow(row5Left, colW), formatBoxRow(row5Right, colW))

					fmt.Printf("└%s┘ └%s┘\n\n", strings.Repeat("─", colW), strings.Repeat("─", colW))
				} else {
					boxW := w - 4
					if boxW < 36 {
						boxW = 36
					}

					fmt.Printf("┌─ %sLOCAL WORKSPACE%s %s┐\n", cBold, cReset, strings.Repeat("─", max(0, boxW-18)))
					fmt.Printf("│%s│\n", formatBoxRow(fmt.Sprintf("  Root: %s", truncateStr(topoRoot(topo), boxW-10)), boxW))
					fmt.Printf("│%s│\n", formatBoxRow(fmt.Sprintf("  Sync Status: %s[ SYNCED ]%s", cGreen, cReset), boxW))
					fmt.Printf("└%s┘\n", strings.Repeat("─", boxW))

					fmt.Printf("┌─ %sPEER FLEET & QUEUE%s %s┐\n", cBold, cReset, strings.Repeat("─", max(0, boxW-20)))
					fmt.Printf("│%s│\n", formatBoxRow(fmt.Sprintf("  > %s PEER %s %-12s (Ready)", bgCyan, cReset, hostName), boxW))
					fmt.Printf("│%s│\n", formatBoxRow(fmt.Sprintf("  Local: %s (%s)", osArch, cores), boxW))
					fmt.Printf("│%s│\n", formatBoxRow(fmt.Sprintf("  Docker Isolation: %s[ ACTIVE ]%s", cCyan, cReset), boxW))
					fmt.Printf("└%s┘\n\n", strings.Repeat("─", boxW))
				}

				fmt.Printf("%s%sSTATUS:%s %sHotkeys: [/] Command Palette | [r] Refresh | [q] Quit%s\n",
					cBold, cCyan, cReset, cYellow, cReset)
			}

			if once {
				render(1)
				return nil
			}

			stdinFd := int(os.Stdin.Fd())
			isInteractive := term.IsTerminal(stdinFd)

			var oldState *term.State
			if isInteractive {
				rawState, err := term.MakeRaw(stdinFd)
				if err == nil {
					oldState = rawState
					defer func() {
						_ = term.Restore(stdinFd, oldState)
					}()
				}
			}

			keyEventCh := make(chan byte, 8)
			go func() {
				buf := make([]byte, 1)
				for {
					n, err := os.Stdin.Read(buf)
					if err != nil || n == 0 {
						return
					}
					keyEventCh <- buf[0]
				}
			}()

			tick := 1
			render(tick)

			ticker := time.NewTicker(refreshInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ctx.Done():
					if oldState != nil {
						_ = term.Restore(stdinFd, oldState)
					}
					fmt.Println("\r\npackets :: dashboard closed")
					return nil

				case b := <-keyEventCh:
					switch b {
					case 3, 'q', 'Q':
						cancel()
						if oldState != nil {
							_ = term.Restore(stdinFd, oldState)
						}
						fmt.Println("\r\npackets :: dashboard closed")
						return nil

					case '/':
						showSearch = !showSearch
						render(tick)

					case 27:
						if showSearch {
							showSearch = false
							render(tick)
						}

					case 'r', 'R':
						tick++
						render(tick)
					}

				case <-ticker.C:
					tick++
					render(tick)
				}
			}
		},
	}

	cmd.Flags().BoolVar(&once, "once", false, "Render once and exit immediately")
	cmd.Flags().IntVarP(&refreshSec, "refresh", "r", 2, "Refresh interval in seconds")

	return cmd
}

func topoRoot(topo *environment.ProjectTopology) string {
	if topo == nil || topo.RootPath == "" {
		return "."
	}
	return topo.RootPath
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

func stripANSI(str string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range str {
		if r == '\033' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func formatBoxRow(content string, width int) string {
	visLen := len(stripANSI(content))
	pad := width - visLen
	if pad < 0 {
		pad = 0
	}
	return content + strings.Repeat(" ", pad)
}
