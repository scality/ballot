package supervisord

import (
	"errors"
	"strconv"
)

const (
	Stopped = "STOPPED"
	Exited  = "EXITED"
)

type Process struct {
	Name  string
	State string
	PID   int
}

func (proc *Process) updateFromListener(event Event) error {
	name, ok := event.Meta["processname"]
	if !ok {
		return errors.New("processname not found in metadata")
	}

	var pid int
	var str string
	var err error

	if str, ok = event.Meta["pid"]; ok {
		if pid, err = strconv.Atoi(str); err != nil {
			return err
		}
	}

	state := event.State()
	if state == Stopped {
		pid = 0
	}

	proc.Name = name
	proc.State = state
	proc.PID = pid
	return nil
}
