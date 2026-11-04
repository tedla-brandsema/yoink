package yoink

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Options controls one composition. Zero values preserve Parse's flat behavior.
// Recursive calls and calls with explicit budgets use finite default budgets.
type Options struct {
	MaxDepth       int           // 0 defaults to 1; -1 means all; positive values count inclusion levels.
	MaxOperations  int           // All recognized command invocations, including .yoink targets.
	MaxInputBytes  int64         // Cumulative bytes read from root and built-in include sources.
	MaxOutputBytes int64         // Final output bytes, including separators and custom results.
	Timeout        time.Duration // Cooperative overall deadline; noncooperative callbacks can delay return.
}

const (
	DefaultMaxOperations        = 10000
	DefaultMaxInputBytes  int64 = 32 << 20
	DefaultMaxOutputBytes int64 = 32 << 20
	DefaultTimeout              = 30 * time.Second
)

var (
	ErrLimitExceeded = errors.New("composition resource limit exceeded")
	ErrCycle         = errors.New("inclusion cycle")
)

// LimitError identifies a shared invocation budget. It unwraps to ErrLimitExceeded.
type LimitError struct {
	Resource string
	Limit    int64
}

func (e *LimitError) Error() string {
	return fmt.Sprintf("%s: %s (limit %d)", ErrLimitExceeded, e.Resource, e.Limit)
}
func (e *LimitError) Unwrap() error { return ErrLimitExceeded }

func (o Options) normalized() (Options, bool, error) {
	if o.MaxDepth < -1 {
		return o, false, fmt.Errorf("invalid MaxDepth %d: use -1, 0, or a positive depth", o.MaxDepth)
	}
	if o.MaxDepth == 0 {
		o.MaxDepth = 1
	}
	if o.MaxOperations < 0 || o.MaxOperations > 1000000 {
		return o, false, fmt.Errorf("MaxOperations must be between 0 and 1000000")
	}
	if o.MaxInputBytes < 0 || o.MaxInputBytes > 1<<30 {
		return o, false, fmt.Errorf("MaxInputBytes must be between 0 and 1073741824")
	}
	if o.MaxOutputBytes < 0 || o.MaxOutputBytes > 1<<30 {
		return o, false, fmt.Errorf("MaxOutputBytes must be between 0 and 1073741824")
	}
	if o.Timeout < 0 || o.Timeout > 24*time.Hour {
		return o, false, fmt.Errorf("Timeout must be between 0 and 24h")
	}
	bounded := o.MaxDepth != 1 || o.MaxOperations != 0 || o.MaxInputBytes != 0 || o.MaxOutputBytes != 0 || o.Timeout != 0
	if bounded {
		if o.MaxOperations == 0 {
			o.MaxOperations = DefaultMaxOperations
		}
		if o.MaxInputBytes == 0 {
			o.MaxInputBytes = DefaultMaxInputBytes
		}
		if o.MaxOutputBytes == 0 {
			o.MaxOutputBytes = DefaultMaxOutputBytes
		}
		if o.Timeout == 0 {
			o.Timeout = DefaultTimeout
		}
	}
	return o, bounded, nil
}

// ParseWithOptions composes selected sources recursively, preserving their source
// locations. MaxDepth 1 (including zero Options) retains Parse's flat behavior.
// Recursive composition uses a bounded worker pool and shared finite budgets.
// Every worker is joined before return, including after errors or cancellation.
// The root uses name's directory; loaded local symlinks resolve nested includes
// from the canonical target's directory. Custom command results remain literal.
// Input readers, filesystem calls and custom callbacks must finish their own
// blocking operations; cancellation cannot forcibly terminate them.
func ParseWithOptions(ctx context.Context, r io.Reader, name string, opts Options) (string, error) {
	o, bounded, err := opts.normalized()
	if err != nil {
		return "", err
	}
	if !bounded {
		return Parse(ctx, r, name)
	}
	ctx, cancel := context.WithTimeout(ctx, o.Timeout)
	defer cancel()
	return compose(ctx, r, name, o)
}
