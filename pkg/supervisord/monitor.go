package supervisord

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

type Monitor struct {
	l       *Listener
	procs   map[string]*Process
	watches map[string][]chan<- *Process
}

func NewMonitor(r io.Reader, w io.Writer) *Monitor {
	return &Monitor{
		l:       NewListener(r, w),
		procs:   make(map[string]*Process),
		watches: make(map[string][]chan<- *Process),
	}
}

func (m *Monitor) removeWatch(service string, ch chan<- *Process) {
	watches := m.watches[service]
	for i, c := range watches {
		if c == ch {
			m.watches[service] = append(watches[:i], watches[i+1:]...)
			return
		}
	}
}

func (m *Monitor) Watch(name string) (<-chan *Process, func()) {
	ch := make(chan *Process)
	m.watches[name] = append(m.watches[name], ch)

	return ch, func() {
		m.removeWatch(name, ch)
		close(ch)
	}
}

func (m *Monitor) Listen(ctx context.Context) error {
	events := make(chan Event)
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case event := <-events:
				fmt.Fprintf(os.Stderr, "event: %v\n", event)
				if strings.HasPrefix(event.Name(), "PROCESS_STATE_") {
					m.updateProcess(event)
				}
			}
		}
	}()

	return m.l.Listen(events)
}

func (m *Monitor) updateProcess(event Event) {
	proc := &Process{}
	err := proc.updateFromListener(event)
	if err != nil {
		return
	}

	m.procs[proc.Name] = proc

	for _, ch := range m.watches[proc.Name] {
		ch <- proc
	}
}
