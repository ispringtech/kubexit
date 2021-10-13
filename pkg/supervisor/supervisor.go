package supervisor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ispringtech/kubexit/pkg/event"

	"github.com/pkg/errors"
)

type Supervisor struct {
	context       context.Context
	cmd           *exec.Cmd
	sigCh         chan os.Signal
	startStopLock sync.Mutex
	shutdownTimer *time.Timer
}

func New(ctx context.Context, name string, args ...string) *Supervisor {
	// Don't use CommandContext.
	// We want the child process to exit on its own so we can return its exit code.
	// If the child doesn't exit on TERM, then neither should the supervisor.
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return &Supervisor{
		context: ctx,
		cmd:     cmd,
	}
}

func (s *Supervisor) Start() error {
	s.startStopLock.Lock()
	defer s.startStopLock.Unlock()

	event.ContextEventTrace(s.context).AddEvent(fmt.Sprintf("Start: %s", s))
	if err := s.cmd.Start(); err != nil {
		return errors.WithStack(fmt.Errorf("failed to start child process: %v", err))
	}

	// Propegate all signals to the child process
	s.sigCh = make(chan os.Signal, 1)
	signal.Notify(s.sigCh)

	go func() {
		for {
			select {
			case <-s.context.Done():
				event.ContextEventTrace(s.context).AddEvent(fmt.Sprintf("Stop signal propegation %s", s.context.Err()))
				return
			case sig, ok := <-s.sigCh:
				if !ok {
					return
				}
				// log everything but "urgent I/O condition", which gets noisy
				if sig != syscall.SIGURG {
					event.ContextEventTrace(s.context).AddEvent(fmt.Sprintf("Received signal: %v", sig))
				}
				// ignore "child exited" signal
				if sig == syscall.SIGCHLD {
					continue
				}
				err := s.cmd.Process.Signal(sig)
				if err != nil {
					event.ContextEventTrace(s.context).AddEvent(fmt.Sprintf("Signal propegation failed: %v\n", err))
				}
			}
		}
	}()

	return nil
}

func (s *Supervisor) Wait() error {
	defer func() {
		signal.Reset()
		if s.sigCh != nil {
			close(s.sigCh)
		}
		if s.shutdownTimer != nil {
			s.shutdownTimer.Stop()
		}
	}()
	return s.cmd.Wait()
}

func (s *Supervisor) ShutdownNow() error {
	s.startStopLock.Lock()
	defer s.startStopLock.Unlock()

	if !s.isRunning() {
		return nil
	}
	// TODO: Use Process.Kill() instead?
	// Sending Interrupt on Windows is not implemented.
	err := s.cmd.Process.Signal(syscall.SIGKILL)
	if err != nil {
		return errors.WithStack(fmt.Errorf("failed to kill child process: %v", err))
	}
	return nil
}

func (s *Supervisor) ShutdownWithTimeout(timeout time.Duration) error {
	s.startStopLock.Lock()
	defer s.startStopLock.Unlock()

	if !s.isRunning() {
		return nil
	}

	if s.shutdownTimer != nil {
		return errors.New("shutdown already started")
	}

	event.ContextEventTrace(s.context).AddEvent("Terminating child process")
	err := s.cmd.Process.Signal(syscall.SIGTERM)
	if err != nil {
		return errors.WithStack(fmt.Errorf("failed to terminate child process: %v", err))
	}

	s.shutdownTimer = time.AfterFunc(timeout, func() {
		err := s.ShutdownNow()
		if err != nil {
			// TODO: ignorable?
			event.ContextEventTrace(s.context).AddEvent(fmt.Sprintf("Failed after timeout: %v", err))
		}
	})

	return nil
}

func (s *Supervisor) isRunning() bool {
	// Process set by cmd.Start - means started
	// https://golang.org/src/os/exec/exec.go?s=11514:11541#L422
	// ProcessState set by cmd.Wait - means exited
	// https://golang.org/src/os/exec/exec.go?s=14689:14715#L511
	return s.cmd.Process != nil && s.cmd.ProcessState == nil
}

// String joins the command Path and Args and quotes any with spaces
func (s *Supervisor) String() string {
	if s.cmd.Path == "" {
		return ""
	}

	var buffer bytes.Buffer

	quote := strings.ContainsRune(s.cmd.Path, ' ')
	if quote {
		buffer.WriteRune('"')
	}
	buffer.WriteString(s.cmd.Path)
	if quote {
		buffer.WriteRune('"')
	}

	if len(s.cmd.Args) > 1 {
		for _, arg := range s.cmd.Args[1:] {
			buffer.WriteRune(' ')
			quote = strings.ContainsRune(arg, ' ')
			if quote {
				buffer.WriteRune('"')
			}
			buffer.WriteString(arg)
			if quote {
				buffer.WriteRune('"')
			}
		}
	}

	return buffer.String()
}
