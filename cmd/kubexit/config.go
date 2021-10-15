package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
)

// json tags added to be able to Marshall config to json
type config struct {
	Name           string        `json:"name"`
	Graveyard      string        `json:"graveyard"`
	BirthDeps      []string      `json:"birth_deps"`
	DeathDeps      []string      `json:"death_deps"`
	BirthTimeout   time.Duration `json:"birth_timeout"`
	GracePeriod    time.Duration `json:"grace_period"`
	PodName        string        `json:"pod_name"`
	Namespace      string        `json:"namespace"`
	VerboseLevel   int           `json:"verbose_level"`
	InstantLogging bool          `json:"instant_logging"`
}

func parseConfig() (*config, error) {
	var err error

	name := os.Getenv("KUBEXIT_NAME")
	if name == "" {
		return nil, errors.New("missing env var: KUBEXIT_NAME")
	}

	graveyard := os.Getenv("KUBEXIT_GRAVEYARD")
	if graveyard == "" {
		graveyard = "/graveyard"
	} else {
		graveyard = strings.TrimRight(graveyard, "/")
		graveyard = filepath.Clean(graveyard)
	}

	birthDepsStr := os.Getenv("KUBEXIT_BIRTH_DEPS")
	var birthDeps []string
	if birthDepsStr != "" {
		birthDeps = strings.Split(birthDepsStr, ",")
	}

	deathDepsStr := os.Getenv("KUBEXIT_DEATH_DEPS")
	var deathDeps []string
	if deathDepsStr != "" {
		deathDeps = strings.Split(deathDepsStr, ",")
	}

	birthTimeout := 30 * time.Second
	birthTimeoutStr := os.Getenv("KUBEXIT_BIRTH_TIMEOUT")
	if birthTimeoutStr != "" {
		birthTimeout, err = time.ParseDuration(birthTimeoutStr)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse birth timeout")
		}
	}

	gracePeriod := 30 * time.Second
	gracePeriodStr := os.Getenv("KUBEXIT_GRACE_PERIOD")
	if gracePeriodStr != "" {
		gracePeriod, err = time.ParseDuration(gracePeriodStr)
		if err != nil {
			return nil, errors.Wrap(err, "failed to parse grace period")
		}
	}

	podName := os.Getenv("KUBEXIT_POD_NAME")
	if podName == "" && len(birthDeps) > 0 {
		return nil, errors.New("missing env var: KUBEXIT_POD_NAME")
	}

	namespace := os.Getenv("KUBEXIT_NAMESPACE")
	if namespace == "" && len(birthDeps) > 0 {
		return nil, errors.New("missing env var: KUBEXIT_NAMESPACE")
	}

	verboseLevel := 0
	verboseLevelStr := os.Getenv("KUBEXIT_VERBOSE_LEVEL")
	if verboseLevelStr != "" {
		verboseLevel, err = strconv.Atoi(verboseLevelStr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse verbose level %s", verboseLevelStr)
		}
	}

	instantLogging := false
	instantLoggingStr := os.Getenv("KUBEXIT_INSTANT_LOGGING")
	if instantLoggingStr != "" {
		instantLogging, err = strconv.ParseBool(instantLoggingStr)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to parse env instant logging %s", verboseLevelStr)
		}
	}

	return &config{
		Name:           name,
		Graveyard:      graveyard,
		BirthDeps:      birthDeps,
		DeathDeps:      deathDeps,
		BirthTimeout:   birthTimeout,
		GracePeriod:    gracePeriod,
		PodName:        podName,
		Namespace:      namespace,
		VerboseLevel:   verboseLevel,
		InstantLogging: instantLogging,
	}, nil
}
