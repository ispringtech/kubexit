package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/errors"

	"github.com/fsnotify/fsnotify"

	"github.com/ispringtech/kubexit/pkg/event"
	"github.com/ispringtech/kubexit/pkg/kubernetes"
	"github.com/ispringtech/kubexit/pkg/loggerhook"
	"github.com/ispringtech/kubexit/pkg/supervisor"
	"github.com/ispringtech/kubexit/pkg/tombstone"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/watch"

	"github.com/sirupsen/logrus"
)

func main() {
	logger := initLogger()

	config, err := parseConfig()
	if err != nil {
		logger.Fatalf("failed to parse conf: %s", err)
	}

	logger.WithField("config", *config).Info("kubexit initialized")

	os.Exit(runApp(config, logger))
}

// runApp should return exit code
func runApp(config *config, logger *logrus.Logger) int {
	var eventTraces []event.Trace

	var err error

	args := os.Args[1:]
	if len(args) == 0 {
		logger.Errorf("no arguments found")
		return 2
	}

	tbEventTrace := event.NewTrace(fmt.Sprintf("%s tombstone", config.Name))
	eventTraces = append(eventTraces, tbEventTrace)

	tombstoneCtx := event.WithEventTrace(
		context.Background(),
		tbEventTrace,
	)
	ts := &tombstone.Tombstone{
		Context:   tombstoneCtx,
		Graveyard: config.Graveyard,
		Name:      config.Name,
	}

	supervisorTrace := event.NewTrace("supervisor")
	eventTraces = append(eventTraces, supervisorTrace)

	child := supervisor.New(event.WithEventTrace(context.Background(), supervisorTrace), args[0], args[1:]...)

	// watch for death deps early, so they can interrupt waiting for birth deps
	if len(config.DeathDeps) > 0 {
		ctx, stopGraveyardWatcher := context.WithCancel(context.Background())
		// stop graveyard watchers on exit, if not sooner
		defer stopGraveyardWatcher()

		graveyardWatcherTrace := event.NewTrace("death graveyard watcher")

		eventTraces = append(eventTraces, graveyardWatcherTrace)

		ctx = event.WithEventTrace(ctx, graveyardWatcherTrace)

		err = tombstone.Watch(ctx, config.Graveyard, onDeathOfAny(config.DeathDeps, func() error {
			stopGraveyardWatcher()
			// trigger graceful shutdown
			// Skipped if not started.
			err2 := child.ShutdownWithTimeout(config.GracePeriod)
			// ShutdownWithTimeout doesn't block until timeout
			if err2 != nil {
				return errors.Wrapf(err2, "failed to shutdown")
			}
			return nil
		}))
		if err != nil {
			return fatalf(logger, eventTraces, child, ts, errors.Wrap(err, "failed to watch graveyard"))
		}
	}

	if len(config.BirthDeps) > 0 {
		ctx := context.Background()

		graveyardWatcherTrace := event.NewTrace("birth dependencies watcher")

		eventTraces = append(eventTraces, graveyardWatcherTrace)

		ctx = event.WithEventTrace(ctx, graveyardWatcherTrace)

		err = waitForBirthDeps(ctx, config.BirthDeps, config.Namespace, config.PodName, config.BirthTimeout)
		if err != nil {
			return fatalf(logger, eventTraces, child, ts, err)
		}
	}

	err = child.Start()
	if err != nil {
		return fatalf(logger, eventTraces, child, ts, err)
	}

	err = ts.RecordBirth()
	if err != nil {
		return fatalf(logger, eventTraces, child, ts, err)
	}

	code := waitForChildExit(child)

	err = ts.RecordDeath(code)
	if err != nil {
		logger.WithError(err).Error()
		return 2
	}

	if config.VerboseLevel > 0 {
		messages, err2 := serializeEventTraces(eventTraces)
		if err2 != nil {
			logger.WithError(err).Error()
			return 2
		}

		logger.WithField("event-traces", messages).Info("supervising proceed successfully")
	}

	return code
}

func waitForBirthDeps(ctx context.Context, birthDeps []string, namespace, podName string, timeout time.Duration) error {
	// Cancel context on SIGTERM to trigger graceful exit
	ctx = withCancelOnSignal(ctx, syscall.SIGTERM)

	ctx, stopPodWatcher := context.WithTimeout(ctx, timeout)
	// Stop pod watcher on exit, if not sooner
	defer stopPodWatcher()

	event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Watching pod %s updates", podName))
	err := kubernetes.WatchPod(
		ctx,
		namespace,
		podName,
		onReadyOfAll(birthDeps, stopPodWatcher),
	)
	if err != nil {
		return errors.Wrap(err, "failed to watch pod")
	}

	// Block until all birth deps are ready
	<-ctx.Done()
	err = ctx.Err()
	if err == context.DeadlineExceeded {
		return errors.WithStack(fmt.Errorf("timed out waiting for birth deps to be ready: %s", timeout))
	} else if err != nil && err != context.Canceled {
		// ignore canceled. shouldn't be other errors, but just in case...
		return errors.WithStack(fmt.Errorf("waiting for birth deps to be ready: %v", err))
	}

	event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("All birth deps ready: %v\n", strings.Join(birthDeps, ", ")))
	return nil
}

// withCancelOnSignal calls cancel when one of the specified signals is received.
func withCancelOnSignal(ctx context.Context, signals ...os.Signal) context.Context {
	ctx, cancel := context.WithCancel(ctx)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, signals...)

	// Trigger context cancel on SIGTERM
	go func() {
		for {
			select {
			case _, ok := <-sigCh:
				if !ok {
					return
				}
				cancel()
			case <-ctx.Done():
				signal.Reset()
				close(sigCh)
			}
		}
	}()

	return ctx
}

// wait for the child to exit and return the exit code
func waitForChildExit(child *supervisor.Supervisor) int {
	var code int
	err := child.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ProcessState.ExitCode()
		} else {
			code = -1
		}
	} else {
		code = 0
	}
	return code
}

// fatalf is for terminal errors.
// Returns exit code
// The child process may or may not be running.
func fatalf(
	logger *logrus.Logger,
	eventTraces []event.Trace,
	child *supervisor.Supervisor,
	ts *tombstone.Tombstone,
	err error,
) int {
	const exitCode = 1

	defer func() {
		messages, err2 := serializeEventTraces(eventTraces)
		if err2 != nil {
			logger.WithError(errors.Wrap(err, err2.Error())).Error()
			return
		}

		logger.WithField("event-traces", messages).WithError(err).Error()
	}()

	// Skipped if not started.
	stopError := child.ShutdownNow()
	if stopError != nil {
		err = errors.Wrap(err, stopError.Error())
		return exitCode
	}

	// Wait for shutdown...
	//TODO: timout in case the process is zombie?
	code := waitForChildExit(child)

	// Attempt to record death, if possible.
	// Another process may be waiting for it.
	recordDeathErr := ts.RecordDeath(code)
	if recordDeathErr != nil {
		err = errors.Wrap(err, recordDeathErr.Error())
		return exitCode
	}

	return exitCode
}

// onReadyOfAll returns an EventHandler that executes the callback when all of
// the birthDeps containers are ready.
func onReadyOfAll(birthDeps []string, callback func()) kubernetes.EventHandler {
	birthDepSet := map[string]struct{}{}
	for _, depName := range birthDeps {
		birthDepSet[depName] = struct{}{}
	}

	return func(ctx context.Context, e watch.Event) {
		// ignore Deleted (Watch will auto-stop on delete)
		if e.Type == watch.Deleted {
			return
		}

		pod, ok := e.Object.(*corev1.Pod)
		if !ok {
			event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Error: unexpected non-pod object type: %+v\n", e.Object))
			return
		}

		// Convert ContainerStatuses list to map of ready container names
		readyContainers := map[string]struct{}{}
		for _, status := range pod.Status.ContainerStatuses {
			if status.Ready {
				readyContainers[status.Name] = struct{}{}
			}
		}

		// Check if all birth deps are ready
		for _, name := range birthDeps {
			if _, ok := readyContainers[name]; !ok {
				// at least one birth dep is not ready
				return
			}
		}

		callback()
	}
}

// onDeathOfAny returns an EventHandler that executes the callback when any of
// the deathDeps processes have died.
func onDeathOfAny(deathDeps []string, callback func() error) tombstone.EventHandler {
	deathDepSet := map[string]struct{}{}
	for _, depName := range deathDeps {
		deathDepSet[depName] = struct{}{}
	}

	return func(ctx context.Context, e fsnotify.Event) error {
		if e.Op&fsnotify.Create != fsnotify.Create && e.Op&fsnotify.Write != fsnotify.Write {
			// ignore other events
			return nil
		}
		graveyard := filepath.Dir(e.Name)
		name := filepath.Base(e.Name)

		if _, ok := deathDepSet[name]; !ok {
			event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Ignore tombstone %s", name))
			// ignore other tombstones
			return nil
		}

		event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Reading tombstone: %s", name))
		ts, err := tombstone.Read(graveyard, name)
		if err != nil {
			return errors.Wrapf(err, "failed to read tombstone %s", name)
		}

		if ts.Died == nil {
			// still alive
			return nil
		}
		event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("New death: %s", name))

		return callback()
	}
}

func initLogger() *logrus.Logger {
	impl := logrus.New()
	impl.SetFormatter(&logrus.JSONFormatter{
		TimestampFormat: time.RFC3339Nano,
		FieldMap: logrus.FieldMap{
			logrus.FieldKeyTime: "@timestamp",
			logrus.FieldKeyMsg:  "message",
		},
	})
	impl.SetLevel(logrus.InfoLevel)
	impl.AddHook(new(loggerhook.StackTraceHook))

	return impl
}

func serializeEventTraces(traces []event.Trace) ([]json.RawMessage, error) {
	messages := make([]json.RawMessage, 0, len(traces))
	for _, trace := range traces {
		message, err2 := trace.Fire()
		if err2 != nil {
			return nil, errors.Wrapf(err2, "failed to marshal event trace: %s", trace.ID())
		}
		messages = append(messages, message)
	}

	return messages, nil
}
