package event

import (
	"context"
	"encoding/json"
	"sync"
	"time"
)

type Event interface {
	Time() time.Time
	Message() string
}

func newEvent(message string) Event {
	return &event{
		time:    time.Now(),
		message: message,
	}
}

type event struct {
	time    time.Time
	message string
}

func (e event) Time() time.Time {
	return e.time
}

func (e event) Message() string {
	return e.message
}

type eventTraceKey struct{}

func WithEventTrace(ctx context.Context, tr Trace) context.Context {
	return context.WithValue(ctx, eventTraceKey{}, tr)
}

func ContextEventTrace(ctx context.Context) Trace {
	tr, ok := ctx.Value(eventTraceKey{}).(Trace)
	if !ok {
		return &noopTrace{}
	}
	return tr
}

func NewTrace(id string) Trace {
	return &trace{id: id}
}

type Trace interface {
	ID() string
	AddEvent(message string)
	Fire() (json.RawMessage, error)
}

type trace struct {
	id     string
	events []Event
	m      sync.Mutex
}

func (t *trace) ID() string {
	return t.id
}

func (t *trace) AddEvent(message string) {
	t.m.Lock()
	defer t.m.Unlock()
	t.events = append(t.events, newEvent(message))
}

func (t *trace) Fire() (json.RawMessage, error) {
	records := make([]interface{}, 0, len(t.events))
	for _, e := range t.events {
		records = append(records, struct {
			Timestamp time.Time `json:"timestamp"`
			Message   string    `json:"message,omitempty"`
		}{
			Timestamp: e.Time(),
			Message:   e.Message(),
		})
	}

	return json.Marshal(struct {
		ID     string        `json:"id"`
		Events []interface{} `json:"events"`
	}{
		ID:     t.id,
		Events: records,
	})
}
