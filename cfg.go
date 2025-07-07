package yoink

import (
	"time"
)

// Config holds global configuration options for the package.
type Config struct {
	MaxConcurrent int
	MinInterval   time.Duration
}

var config *Config

// DefaultConfig returns a Config with logical, safe defaults.
func DefaultConfig() *Config {
	return &Config{
		MaxConcurrent: 0,
		MinInterval:   0 * time.Millisecond,
	}
}

// InitConfig initializes the global config instance.
// Should be called during package init or main().
func InitConfig(cfg *Config) {
	config = cfg
}

func init() {
	InitConfig(DefaultConfig())
}
