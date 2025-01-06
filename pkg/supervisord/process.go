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

func ProcessFromEvent(event Event) (proc Process, err error) {
	name, ok := event.Meta["processname"]
	if !ok {
		err = errors.New("processname not found in metadata")
		return
	}

	var pid int
	var str string

	if str, ok = event.Meta["pid"]; ok {
		if pid, err = strconv.Atoi(str); err != nil {
			return
		}
	}

	state := event.State()
	if state == Stopped || state == Exited {
		pid = 0
	}

	proc.Name = name
	proc.State = state
	proc.PID = pid
	return
}
