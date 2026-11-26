//go:build unix

package backendcore

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/MrAmperage/ReportBoxBackendCore/app"
	"github.com/MrAmperage/ReportBoxBackendCore/config"
	"github.com/MrAmperage/ReportBoxBackendCore/lifecycle"
	"go.uber.org/fx"
)

func TestRunChild(t *testing.T) {
	mode := os.Getenv("BACKENDCORE_TEST_CHILD")
	if mode == "" {
		return
	}
	cfg := config.Defaults()
	if mode == "invalid" {
		cfg.StartTimeout = 0
		NewWithConfig(app.Info{Name: "child"}, cfg).Run()
		return
	}
	a := NewWithConfig(app.Info{Name: "child"}, cfg, fx.Provide(fx.Annotate(func() lifecycle.Participant {
		return lifecycle.Participant{Name: "child", Drain: func(context.Context) error { fmt.Println("CHILD_DRAINED"); return nil }}
	}, fx.ResultTags(`group:"backendcore.lifecycle"`))))
	// Install Fx signal receiver before the readiness marker.
	a.Wait()
	a.Run()
}
func TestRunSIGTERM(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunChild$")
	cmd.Env = append(os.Environ(), "BACKENDCORE_TEST_CHILD=signal")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = cmd.Stdout
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan struct{})
	done := make(chan struct{})
	var output []string
	go func() {
		defer close(done)
		scanner := bufio.NewScanner(stdout)
		seen := false
		for scanner.Scan() {
			line := scanner.Text()
			output = append(output, line)
			if !seen && strings.Contains(line, "managed lifecycle running") {
				close(ready)
				seen = true
			}
		}
	}()
	select {
	case <-ready:
	case <-done:
		_ = cmd.Wait()
		t.Fatalf("child exited early: %v", output)
	case <-ctx.Done():
		<-done
		_ = cmd.Wait()
		t.Fatalf("child start timeout: %v", output)
	}
	if err = cmd.Process.Signal(syscall.SIGTERM); err != nil {
		cancel()
		<-done
		_ = cmd.Wait()
		t.Fatal(err)
	}
	<-done
	err = cmd.Wait()
	if err != nil || !strings.Contains(strings.Join(output, "\n"), "CHILD_DRAINED") {
		t.Fatalf("shutdown: %v %v", err, output)
	}
}
func TestRunInvalidExitCode(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRunChild$")
	cmd.Env = append(os.Environ(), "BACKENDCORE_TEST_CHILD=invalid")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected nonzero exit")
	} else if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 1 {
		t.Fatalf("unexpected exit: %v", err)
	}
}
