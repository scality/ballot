package supervisord

import (
	"context"
	"io"
	"strings"
	"sync"
)

type Monitor struct {
	l     *Listener
	hooks map[chan<- Process]interface{}
	mu    sync.Mutex
}

func NewMonitor(r io.Reader, w io.Writer, serviceName string) *Monitor {
	return &Monitor{
		l:     NewListener(r, w),
		hooks: make(map[chan<- Process]interface{}),
	}
}

func (m *Monitor) Listen(ctx context.Context) error {
	events := make(chan Event)
	defer close(events)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-events:
				if strings.HasPrefix(event.Name(), "PROCESS_STATE_") {
					proc, err := ProcessFromEvent(event)
					if err != nil {
						continue
					}

					m.mu.Lock()
					for ch := range m.hooks {
						ch <- proc
					}
					m.mu.Unlock()
				}
			}
		}
	}()

	return m.l.Listen(events)
}

func (m *Monitor) WaitForNextExit(ctx context.Context) Process {
	ch := make(chan Process)
	m.mu.Lock()
	m.hooks[ch] = nil
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.hooks, ch)
		m.mu.Unlock()
	}()

	for {
		select {
		case <-ctx.Done():
			return Process{}
		case proc := <-ch:
			if proc.State == Exited || proc.State == Stopped {
				return proc
			}
		}
	}
}
