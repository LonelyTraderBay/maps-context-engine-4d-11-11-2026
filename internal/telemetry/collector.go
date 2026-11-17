package telemetry

import (
	"context"
	"sync"
	"sync/atomic"
	"time"
)

// eventSink receives only already-validated JSONL. Implementations must honor
// cancellation where the underlying operating system permits it.
type eventSink interface {
	appendEvent(context.Context, []byte) error
}

type Collector struct {
	worktree string
	sink     eventSink
	debug    bool
	queue    chan []byte
	stop     chan struct{}
	done     chan struct{}
	ctx      context.Context
	cancel   context.CancelFunc
	mu       sync.Mutex
	closed   bool
	written  atomic.Uint64
	dropped  atomic.Uint64
	invalid  atomic.Uint64
	failures atomic.Uint64
	degraded atomic.Bool
}

type CollectorStats struct{ Written, Dropped, Invalid, SinkFailures uint64 }

// NewCollector starts one worker. Queue capacity is finite; submission never
// waits for the sink. The sink and lease remain owned by the caller, who must
// retain them until Close succeeds.
func NewCollector(sink *FileSink, capacity int, debug bool) (*Collector, error) {
	if sink == nil {
		return nil, ErrInvalid
	}
	return newCollector(sink, sink.worktree, capacity, debug)
}

func newCollector(sink eventSink, worktree string, capacity int, debug bool) (*Collector, error) {
	if sink == nil || !uuid(worktree) || capacity < 1 || capacity > 4096 {
		return nil, ErrInvalid
	}
	ctx, cancel := context.WithCancel(context.Background())
	c := &Collector{sink: sink, worktree: worktree, debug: debug, queue: make(chan []byte, capacity), stop: make(chan struct{}), done: make(chan struct{}), ctx: ctx, cancel: cancel}
	go c.run()
	return c, nil
}

func increment(n *atomic.Uint64) {
	for {
		old := n.Load()
		if old == ^uint64(0) || n.CompareAndSwap(old, old+1) {
			return
		}
	}
}

// Submit returns false when invalid, filtered, closed or dropped. Logging must
// never determine the success of the operation being observed.
func (c *Collector) Submit(e Event) bool {
	if c == nil {
		return false
	}
	if e.Level == "debug" && !c.debug {
		return false
	}
	raw, err := encodeEvent(e, c.worktree, time.Now())
	if err != nil {
		increment(&c.invalid)
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		increment(&c.dropped)
		return false
	}
	select {
	case c.queue <- raw:
		return true
	default:
		increment(&c.dropped)
		c.degraded.Store(true)
		return false
	}
}

func (c *Collector) run() {
	defer close(c.done)
	defer c.cancel()
	for {
		select {
		case raw := <-c.queue:
			c.write(raw)
		case <-c.stop:
			for {
				select {
				case raw := <-c.queue:
					c.write(raw)
				default:
					return
				}
			}
		}
	}
}

func (c *Collector) write(raw []byte) {
	ctx, cancel := context.WithTimeout(c.ctx, 2*time.Second)
	err := c.sink.appendEvent(ctx, raw)
	cancel()
	if err != nil {
		increment(&c.failures)
		c.degraded.Store(true)
		return
	}
	increment(&c.written)
}

func (c *Collector) Stats() CollectorStats {
	if c == nil {
		return CollectorStats{}
	}
	return CollectorStats{c.written.Load(), c.dropped.Load(), c.invalid.Load(), c.failures.Load()}
}

// TakeDiagnostic returns a fixed sanitized diagnostic once per observed
// degradation. A CLI may print it to stderr outside operation locks.
func (c *Collector) TakeDiagnostic() string {
	if c != nil && c.degraded.Swap(false) {
		return "mce4d: logging unavailable or events dropped\n"
	}
	return ""
}

// Close stops submissions and drains queued events until ctx expires. On
// timeout, cancellation is signaled and the sink/lease must remain valid until
// a later successful Close; OS file writes cannot always be interrupted.
func (c *Collector) Close(ctx context.Context) error {
	if c == nil || ctx == nil {
		return ErrInvalid
	}
	c.mu.Lock()
	if !c.closed {
		c.closed = true
		close(c.stop)
	}
	c.mu.Unlock()
	select {
	case <-c.done:
		return nil
	case <-ctx.Done():
		c.cancel()
		return ctx.Err()
	}
}
