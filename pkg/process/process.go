// Copyright 2021 Scality, Inc
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package process

import (
	"bufio"
	"context"
	"io"
	"os/exec"
	"syscall"

	"github.com/sirupsen/logrus"
)

func captureOutput(out io.ReadCloser, writer io.Writer, wrapLogs bool, logFunc func(...interface{}), done chan struct{}) {
	defer func() {
		close(done)
	}()

	if wrapLogs {
		scanner := bufio.NewScanner(out)

		for scanner.Scan() {
			t := scanner.Text()

			logFunc(t)
		}

	} else {
		io.Copy(writer, out)
	}
}

func RunChildProcess(ctx context.Context, args []string, outWriter io.Writer, errWriter io.Writer, wrapLogs bool, log *logrus.Entry) chan RunStatus {
	ret := make(chan RunStatus)

	go func() {
		log.Info("running", args)

		stdoutDone := make(chan struct{})
		stderrDone := make(chan struct{})
		subprocessWaitDone := make(chan struct{})

		status := RunStatus{}

		defer func() {
			ret <- status
		}()

		executable := args[0]

		cmd := exec.Command(executable, args[1:]...)
		cmd.Stdin = nil

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			log.Debugf("stdout pipe err %v", err)

			status.Err = err

			return
		}

		go captureOutput(stdout, outWriter, wrapLogs, log.WithField("name", "cmd-run-stdout").Info, stdoutDone)

		stderr, err := cmd.StderrPipe()
		if err != nil {
			log.Debugf("stderr pipe err %v", err)

			status.Err = err

			return
		}

		go captureOutput(stderr, errWriter, wrapLogs, log.WithField("name", "cmd-run-stderr").Error, stderrDone)

		err = cmd.Start()
		if err != nil {
			log.Debugf("start err %v", err)

			status.Err = err

			return
		}

		go func() {
			defer close(subprocessWaitDone)

			<-stdoutDone
			<-stderrDone

			err = cmd.Wait()
			if err != nil {
				log.Debugf("wait err %v", err)

				status.Err = err
				status.ExitCode = cmd.ProcessState.ExitCode()
			}
		}()

		for {
			select {
			case <-ctx.Done():
				// TODO .Kill() after some timeout
				cmd.Process.Signal(syscall.SIGTERM)

			case <-subprocessWaitDone:
				return
			}
		}
	}()

	return ret
}
