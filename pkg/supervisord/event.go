package supervisord

import (
	"bufio"
	"bytes"
	"fmt"
	"strconv"
	"strings"
)

type Event struct {
	Header  map[string]string
	Meta    map[string]string
	Payload []byte
}

func (event Event) Name() string {
	return event.Header["eventname"]
}

func (e Event) State() string {
	if strings.HasPrefix(e.Name(), "PROCESS_STATE_") {
		return e.Name()[14:]
	}

	return ""
}

func parseKV(line []byte) (map[string]string, error) {
	kv := make(map[string]string)
	trimmed := bytes.TrimSpace(line)
	for _, token := range bytes.Split(trimmed, []byte(" ")) {
		if len(token) == 0 {
			continue
		}

		parts := bytes.SplitN(token, []byte(":"), 2)

		switch len(parts) {
		case 1:
			kv[string(parts[0])] = ""
		case 2:
			kv[string(parts[0])] = string(parts[1])
		default:
			return nil, fmt.Errorf("invalid key-value pair: %s", token)
		}
	}

	return kv, nil
}

func ReadEvent(r *bufio.Reader) (Event, error) {
	headerRaw, err := r.ReadBytes('\n')
	if err != nil {
		return Event{}, err
	}

	header, err := parseKV(headerRaw)
	if err != nil {
		return Event{}, err
	}

	length, err := strconv.Atoi(header["len"])
	if err != nil {
		return Event{}, err
	}

	payloadRaw := make([]byte, length)
	_, err = r.Read(payloadRaw)
	if err != nil {
		return Event{}, err
	}

	var meta map[string]string
	var payload []byte

	if index := bytes.IndexByte(payloadRaw, '\n'); index > 0 {
		meta, err = parseKV(payloadRaw[:index])
		payload = payloadRaw[index+1:]
	} else {
		meta, err = parseKV(payloadRaw)
	}

	if err != nil {
		return Event{}, err
	}

	return Event{
		Header:  header,
		Meta:    meta,
		Payload: payload,
	}, nil
}
