package yoink

import (
	"time"
)

type Configurable interface {
	Configure()
}

type config struct {
	MaxConcurrent int
	MinInterval   time.Duration
}

// Config holds the legacy startup defaults. The built-in remote parser takes
// its configuration during package initialization; later changes do not
// reconfigure it. Parsing does not read Config.
var Config *config

const (
	defaultMaxConcurrent = 5
	defaultMinInterval   = 200 * time.Millisecond
)

func defaultConfig() *config {
	return &config{
		MaxConcurrent: defaultMaxConcurrent,
		MinInterval:   defaultMinInterval,
	}
}

func init() {
	Config = defaultConfig()
}
