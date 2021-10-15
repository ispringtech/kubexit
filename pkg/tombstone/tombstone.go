package tombstone

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"sigs.k8s.io/yaml"

	"github.com/pkg/errors"

	"github.com/ispringtech/kubexit/pkg/event"
)

type Tombstone struct {
	Context context.Context `json:"-"`

	Born     *time.Time `json:",omitempty"`
	Died     *time.Time `json:",omitempty"`
	ExitCode *int       `json:",omitempty"`

	Graveyard string `json:"-"`
	Name      string `json:"-"`

	fileLock sync.Mutex
}

func (t *Tombstone) Path() string {
	return filepath.Join(t.Graveyard, t.Name)
}

// Write a tombstone file, truncating before writing.
// If the FilePath directories do not exist, they will be created.
func (t *Tombstone) Write() error {
	// one write at a time
	t.fileLock.Lock()
	defer t.fileLock.Unlock()

	err := os.MkdirAll(t.Graveyard, os.ModePerm)
	if err != nil {
		return err
	}

	// does not exit
	file, err := os.Create(t.Path())
	if err != nil {
		return fmt.Errorf("failed to create tombstone file: %v", err)
	}
	defer file.Close()

	pretty, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Errorf("failed to marshal tombstone yaml: %v", err)
	}
	_, _ = file.Write(pretty)
	return nil
}

func (t *Tombstone) RecordBirth() error {
	born := time.Now()
	t.Born = &born

	event.ContextEventTrace(t.Context).AddEvent(fmt.Sprintf("Creating tombstone: %s", t.Path()))
	err := t.Write()
	if err != nil {
		return errors.WithStack(fmt.Errorf("failed to create tombstone: %v", err))
	}
	return nil
}

func (t *Tombstone) RecordDeath(exitCode int) error {
	code := exitCode
	died := time.Now()
	t.Died = &died
	t.ExitCode = &code

	event.ContextEventTrace(t.Context).AddEvent(fmt.Sprintf("Updating tombstone: %s", t.Path()))
	err := t.Write()
	if err != nil {
		return errors.WithStack(fmt.Errorf("failed to update tombstone: %v", err))
	}
	return nil
}

func (t *Tombstone) String() string {
	inline, err := json.Marshal(t)
	if err != nil {
		event.ContextEventTrace(t.Context).AddEvent(fmt.Sprintf("Error: failed to marshal tombstone as json: %v", err))
		return "{}"
	}
	return string(inline)
}

// Read a tombstone from a graveyard.
func Read(graveyard, name string) (*Tombstone, error) {
	t := Tombstone{
		Graveyard: graveyard,
		Name:      name,
	}

	bytes, err := ioutil.ReadFile(t.Path())
	if err != nil {
		return nil, errors.WithStack(fmt.Errorf("failed to read tombstone file: %v", err))
	}

	err = yaml.Unmarshal(bytes, &t)
	if err != nil {
		return nil, errors.WithStack(fmt.Errorf("failed to unmarshal tombstone yaml: %v", err))
	}

	return &t, nil
}

type EventHandler func(context.Context, fsnotify.Event) error

// Watch a graveyard and call the eventHandler (asyncronously) when an
// event happens. When the supplied context is canceled, watching will stop.
func Watch(ctx context.Context, graveyard string, eventHandler EventHandler) error {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.WithStack(fmt.Errorf("failed to create watcher: %v", err))
	}

	go func() {
		defer watcher.Close()
		for {
			select {
			case <-ctx.Done():
				event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Tombstone Watch(%s): done", graveyard))
				return
			case e, ok := <-watcher.Events:
				if !ok {
					return
				}
				err = eventHandler(ctx, e)
				if err != nil {
					event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Handler error: %s", err))
				}
			case err2, ok := <-watcher.Errors:
				if !ok {
					return
				}
				event.ContextEventTrace(ctx).AddEvent(fmt.Sprintf("Tombstone Watch(%s): error: %v", graveyard, err2))
				// TODO: wrap ctx with WithCancel and cancel on terminal errors, if any
			}
		}
	}()

	err = watcher.Add(graveyard)
	if err != nil {
		return errors.WithStack(fmt.Errorf("failed to add watcher: %v", err))
	}
	return nil
}
