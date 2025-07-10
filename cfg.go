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
