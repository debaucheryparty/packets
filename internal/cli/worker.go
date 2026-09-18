package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/debaucheryparty/packets/internal/config"
	pb "github.com/debaucheryparty/packets/proto/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func NewWorkerCommand(_ *config.Config, _ *slog.Logger) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worker",
		Short: "Manage peer compute workers and connect peer laptops to Packets",
	}

	cmd.AddCommand(
		newWorkerJoinCommand(),
		newWorkerStatusCommand(),
	)

	return cmd
}

func newWorkerJoinCommand() *cobra.Command {
	var name string
	var maxJobs int
	var dockerOnly bool
	var heartbeatSec int

	cmd := &cobra.Command{
		Use:   "join <scheduler-addr>",
		Short: "Join a remote Packets scheduler as a peer compute worker",
		Example: `  packets worker join 100.64.0.1:50051
  packets worker join 127.0.0.1:50051 --name friend-pc --max-jobs 2`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			schedulerAddr := args[0]
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer cancel()

			if name == "" {
				h, err := os.Hostname()
				if err != nil || h == "" {
					name = fmt.Sprintf("peer-%d", time.Now().Unix()%10000)
				} else {
					name = h
				}
			}

			fmt.Printf("packets :: connecting to scheduler at %s...\n", schedulerAddr)

			conn, err := grpc.NewClient(schedulerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
			if err != nil {
				return fmt.Errorf("failed to create client for scheduler %s: %w", schedulerAddr, err)
			}
			defer func() { _ = conn.Close() }()

			client := pb.NewSchedulerClient(conn)
			stream, err := client.RegisterWorkerStream(ctx)
			if err != nil {
				return fmt.Errorf("failed to connect worker stream to %s: %w", schedulerAddr, err)
			}

			cores := runtime.NumCPU()
			osArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)
			workerID := fmt.Sprintf("peer-%s-%d", name, time.Now().Unix()%100000)

			outCh := make(chan *pb.WorkerMsg, 64)
			sendErrCh := make(chan error, 1)

			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case msg, ok := <-outCh:
						if !ok {
							return
						}
						if err := stream.Send(msg); err != nil {
							sendErrCh <- err
							return
						}
					}
				}
			}()

			outCh <- &pb.WorkerMsg{
				Payload: &pb.WorkerMsg_Hello{
					Hello: &pb.WorkerHello{
						WorkerId:   workerID,
						NodeName:   name,
						Arch:       osArch,
						Cores:      int32(cores),
						MaxJobs:    int32(maxJobs),
						DockerOnly: dockerOnly,
					},
				},
			}

			firstMsg, err := stream.Recv()
			if err != nil {
				return fmt.Errorf("failed to receive registration ack: %w", err)
			}
			if ack := firstMsg.GetAck(); ack != nil && !ack.Success {
				return fmt.Errorf("registration rejected: %s", ack.Message)
			}

			fmt.Printf("packets :: peer worker registered successfully\n")
			fmt.Printf("packets :: node name: %s (id: %s)\n", name, workerID)
			fmt.Printf("packets :: architecture: %s (cores: %d)\n", osArch, cores)
			fmt.Printf("packets :: max concurrent jobs: %d\n", maxJobs)
			if dockerOnly {
				fmt.Printf("packets :: security mode: containerized (docker only)\n")
			} else {
				fmt.Printf("packets :: security mode: standard\n")
			}
			fmt.Printf("packets :: worker online and waiting for jobs (press Ctrl+C to exit)\n")

			go func() {
				ticker := time.NewTicker(time.Duration(heartbeatSec) * time.Second)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						select {
						case outCh <- &pb.WorkerMsg{
							Payload: &pb.WorkerMsg_Heartbeat{
								Heartbeat: &pb.WorkerHeartbeat{WorkerId: workerID},
							},
						}:
						case <-ctx.Done():
							return
						}
					}
				}
			}()

			sem := make(chan struct{}, maxJobs)
			for {
				select {
				case <-ctx.Done():
					fmt.Println("\npackets :: disconnecting peer worker gracefully...")
					return nil
				case err := <-sendErrCh:
					return fmt.Errorf("worker send failed: %w", err)
				default:
				}

				msg, err := stream.Recv()
				if err != nil {
					if ctx.Err() != nil {
						return nil
					}
					return fmt.Errorf("stream closed by scheduler: %w", err)
				}

				if assign := msg.GetAssign(); assign != nil {
					fmt.Printf("packets :: [%s] received job %s (toolchain: %s)\n",
						time.Now().Format(time.TimeOnly), assign.JobId, assign.Toolchain)

					sem <- struct{}{}
					go func(a *pb.JobAssignment) {
						defer func() { <-sem }()
						executeWorkerTask(ctx, a, outCh)
						fmt.Printf("packets :: [%s] completed job %s\n",
							time.Now().Format(time.TimeOnly), a.JobId)
					}(assign)
				}
			}
		},
	}

	cmd.Flags().StringVar(&name, "name", "", "friendly name for this peer compute node")
	cmd.Flags().IntVar(&maxJobs, "max-jobs", 2, "maximum concurrent jobs this worker will accept")
	cmd.Flags().BoolVar(&dockerOnly, "docker-only", true, "enforce docker container execution for peer safety")
	cmd.Flags().IntVar(&heartbeatSec, "heartbeat", 5, "heartbeat interval in seconds")

	return cmd
}

func newWorkerStatusCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Display local worker node capabilities and hardware specs",
		RunE: func(cmd *cobra.Command, args []string) error {
			hostname, _ := os.Hostname()
			cores := runtime.NumCPU()
			osArch := fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH)

			fmt.Println("============================================================")
			fmt.Println("PACKETS PEER COMPUTE CAPABILITIES")
			fmt.Println("============================================================")
			fmt.Printf("Host Name:    %s\n", hostname)
			fmt.Printf("OS/Arch:      %s\n", osArch)
			fmt.Printf("CPU Cores:    %d\n", cores)
			fmt.Printf("Go Runtime:   %s\n", runtime.Version())
			fmt.Println("Isolation:    Docker container isolation enforced by default")
			fmt.Println("============================================================")
			return nil
		},
	}
}

func executeWorkerTask(ctx context.Context, assign *pb.JobAssignment, outCh chan<- *pb.WorkerMsg) {
	cmdArgs := assign.CommandArgs
	if len(cmdArgs) == 0 {
		cmdArgs = []string{"echo", "no command specified"}
	}

	cmd := exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		outCh <- &pb.WorkerMsg{
			Payload: &pb.WorkerMsg_Result{
				Result: &pb.WorkerJobResult{
					JobId:        assign.JobId,
					ExitCode:     1,
					ErrorMessage: err.Error(),
				},
			},
		}
		return
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		outCh <- &pb.WorkerMsg{
			Payload: &pb.WorkerMsg_Result{
				Result: &pb.WorkerJobResult{
					JobId:        assign.JobId,
					ExitCode:     1,
					ErrorMessage: err.Error(),
				},
			},
		}
		return
	}

	var stdoutBuf, stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	scanPipe := func(r io.Reader, buf *strings.Builder) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			line := scanner.Text()
			buf.WriteString(line + "\n")
			select {
			case outCh <- &pb.WorkerMsg{
				Payload: &pb.WorkerMsg_Log{
					Log: &pb.WorkerJobLog{JobId: assign.JobId, Line: line},
				},
			}:
			case <-ctx.Done():
				return
			}
		}
	}

	go scanPipe(stdoutPipe, &stdoutBuf)
	go scanPipe(stderrPipe, &stderrBuf)

	if err := cmd.Start(); err != nil {
		outCh <- &pb.WorkerMsg{
			Payload: &pb.WorkerMsg_Result{
				Result: &pb.WorkerJobResult{
					JobId:        assign.JobId,
					ExitCode:     1,
					ErrorMessage: err.Error(),
				},
			},
		}
		return
	}

	wg.Wait()
	waitErr := cmd.Wait()

	exitCode := 0
	errMsg := ""
	if waitErr != nil {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
			errMsg = waitErr.Error()
		}
	}

	outCh <- &pb.WorkerMsg{
		Payload: &pb.WorkerMsg_Result{
			Result: &pb.WorkerJobResult{
				JobId:        assign.JobId,
				ExitCode:     int32(exitCode),
				Stdout:       stdoutBuf.String(),
				Stderr:       stderrBuf.String(),
				ErrorMessage: errMsg,
			},
		},
	}
}
