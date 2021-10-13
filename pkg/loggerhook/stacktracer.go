package loggerhook

import (
	"fmt"

	"github.com/pkg/errors"
)

type stackTracer interface {
	StackTrace() errors.StackTrace
}

func GetStackTrace(err error) []string {
	tracer := getOldestStackTracer(err)
	if tracer == nil {
		return nil
	}

	stackTrace := tracer.StackTrace()
	return toStringSlice(stackTrace)
}

func toStringSlice(stackTrace errors.StackTrace) (result []string) {
	for _, f := range stackTrace {
		result = append(result, fmt.Sprintf("%+v", f))
	}
	return
}

func getOldestStackTracer(err error) (oldestStackTracer stackTracer) {
	for err != nil {
		if tracer, ok := err.(stackTracer); ok {
			oldestStackTracer = tracer
		}

		err = getCause(err)
	}

	return
}

type causer interface {
	Cause() error
}

func getCause(err error) error {
	if c, ok := err.(causer); ok {
		return c.Cause()
	}

	return nil
}
