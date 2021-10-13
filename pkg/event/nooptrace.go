package event

import "encoding/json"

type noopTrace struct{}

func (n noopTrace) ID() string {
	//	Do nothing
	return ""
}

func (n noopTrace) AddEvent(string) {
	//	Do nothing
}

func (n noopTrace) Fire() (json.RawMessage, error) {
	//	Do nothing
	return nil, nil
}
