package supervisord

import (
	"bufio"
	"io"
)

type Listener struct {
	r *bufio.Reader
	w io.Writer
}

func NewListener(r io.Reader, w io.Writer) *Listener {
	return &Listener{r: bufio.NewReader(r), w: w}
}

func (l *Listener) Read() (Event, error) {
	return ReadEvent(l.r)
}

func (l *Listener) Ready() error {
	_, err := l.w.Write([]byte("READY\n"))
	return err
}

func (l *Listener) OK() error {
	_, err := l.w.Write([]byte("RESULT 2\nOK"))
	return err
}

func (l *Listener) Fail() error {
	_, err := l.w.Write([]byte("RESULT 4\nFAIL"))
	return err
}

func (l *Listener) Listen(events chan<- Event) error {
	for {
		if err := l.Ready(); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}

		event, err := l.Read()
		if err != nil {
			return err
		}

		events <- event

		if err := l.OK(); err != nil {
			return err
		}
	}
}
