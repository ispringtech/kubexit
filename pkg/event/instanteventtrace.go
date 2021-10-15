package event

import (
	"github.com/sirupsen/logrus"
)

func NewInstantTrace(id string, logger *logrus.Entry) Trace {
	return &instantEventTrace{
		trace:  &trace{id: id},
		logger: logger,
	}
}

type instantEventTrace struct {
	*trace

	logger *logrus.Entry
}

func (trace *instantEventTrace) AddEvent(message string) {
	trace.m.Lock()
	defer trace.m.Unlock()
	trace.events = append(trace.events, newEvent(message))
	trace.logger.WithField("event-trace-id", trace.id).WithField("event", message).Trace()
}
