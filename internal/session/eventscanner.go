package session

import (
	"io"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zevro-ai/remote-control-on-demand/internal/config"
)

const defaultThrottleInterval = 30 * time.Second

// eventPattern is a compiled pattern with tracking state.
type eventPattern struct {
	name     string
	regex    *regexp.Regexp
	once     bool
	fired    atomic.Bool
	mu       sync.Mutex
	lastFire time.Time
}

// eventScanner wraps an io.Writer, matches regex patterns in written data
// and detects idle periods (no output for a configurable duration).
type eventScanner struct {
	inner            io.Writer
	patterns         []*eventPattern
	patternCallback  func(patternName, match string)
	activityCallback func()
	throttleInterval time.Duration

	// Idle detection
	idleTimeout  time.Duration
	idleCallback func()
	idleTimer    *time.Timer
	idleFired    atomic.Bool

	mu      sync.Mutex
	stopped chan struct{}
}

func newEventScanner(
	inner io.Writer,
	cfg *config.NotificationsConfig,
	patternCallback func(patternName, match string),
	activityCallback func(),
	idleCallback func(),
) *eventScanner {
	es := &eventScanner{
		inner:            inner,
		patternCallback:  patternCallback,
		activityCallback: activityCallback,
		idleCallback:     idleCallback,
		throttleInterval: defaultThrottleInterval,
		stopped:          make(chan struct{}),
	}

	if cfg == nil {
		return es
	}

	for _, p := range cfg.Patterns {
		compiled, _ := regexp.Compile(p.Regex) // already validated
		es.patterns = append(es.patterns, &eventPattern{
			name:  p.Name,
			regex: compiled,
			once:  p.Once,
		})
	}

	es.idleTimeout = time.Duration(cfg.IdleTimeout)
	if es.idleTimeout > 0 && idleCallback != nil {
		es.idleTimer = time.AfterFunc(es.idleTimeout, es.onIdle)
	}

	return es
}

func (es *eventScanner) Write(p []byte) (n int, err error) {
	n, err = es.inner.Write(p)

	if len(p) > 0 && es.activityCallback != nil {
		es.activityCallback()
	}

	// Reset idle state
	if es.idleTimeout > 0 {
		es.idleFired.Store(false)
		es.mu.Lock()
		if es.idleTimer != nil {
			es.idleTimer.Reset(es.idleTimeout)
		}
		es.mu.Unlock()
	}

	// Check patterns
	now := time.Now()
	for _, pat := range es.patterns {
		if pat.once && pat.fired.Load() {
			continue
		}

		if match := pat.regex.Find(p); match != nil {
			if pat.once {
				if pat.fired.CompareAndSwap(false, true) {
					es.patternCallback(pat.name, string(match))
				}
			} else {
				pat.mu.Lock()
				if now.Sub(pat.lastFire) >= es.throttleInterval {
					pat.lastFire = now
					pat.mu.Unlock()
					es.patternCallback(pat.name, string(match))
				} else {
					pat.mu.Unlock()
				}
			}
		}
	}

	return
}

func (es *eventScanner) onIdle() {
	if es.idleFired.CompareAndSwap(false, true) {
		select {
		case <-es.stopped:
			return
		default:
			es.idleCallback()
		}
	}
}

// Stop prevents further callbacks and stops the idle timer.
func (es *eventScanner) Stop() {
	select {
	case <-es.stopped:
		return // already stopped
	default:
		close(es.stopped)
	}
	es.mu.Lock()
	if es.idleTimer != nil {
		es.idleTimer.Stop()
	}
	es.mu.Unlock()
}
