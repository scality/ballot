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
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/sirupsen/logrus"
)

var _ = Describe("Process", func() {
	trueCmd := []string{"sh", "-c", "true"}
	trueCmdTimeout := 5 * time.Second

	nonexistentCmd := []string{"/nonexistentcmd4587395"}
	nonexistentCmdTimeout := 5 * time.Second

	failCmdExitCode := 4
	failCmd := []string{"sh", "-c", fmt.Sprintf("exit %d", failCmdExitCode)}
	failCmdTimeout := 5 * time.Second

	sleepCmdTimeout := 5 * time.Second
	sleepCmd := []string{"sh", "-c", fmt.Sprintf("sleep %v", 2*sleepCmdTimeout.Seconds())}

	outMessage := "out"
	errMessage := "err"
	outputCmdTimeout := 5 * time.Second
	outputCmd := []string{"sh", "-c", fmt.Sprintf("echo %v && echo %v >&2", outMessage, errMessage)}

	var ctx context.Context
	var logger *logrus.Logger
	var log *logrus.Entry
	var outWriter, errWriter *bytes.Buffer
	var logOutWriter *bytes.Buffer

	BeforeEach(func() {
		ctx = context.Background()

		logOutWriter = bytes.NewBuffer(nil)

		logger = logrus.New()
		logger.ReportCaller = true
		logger.SetFormatter(&logrus.JSONFormatter{})
		logger.SetOutput(logOutWriter)
		logger.SetLevel(logrus.TraceLevel)
		log = logrus.NewEntry(logger)

		outWriter = bytes.NewBuffer(nil)
		errWriter = bytes.NewBuffer(nil)
	})

	Describe("RunChildProcess", func() {
		It("should pass successful completion in channel", func() {
			ch := RunChildProcess(ctx, trueCmd, outWriter, errWriter, false, log)

			select {
			case <-time.After(trueCmdTimeout):
				Fail("command timed out")

			case status, ok := <-ch:
				Expect(ok).To(BeTrue())
				Expect(status.Err).NotTo(HaveOccurred())
				Expect(status.ExitCode).To(BeZero())
			}
		})

		It("should pass exec failure in channel", func() {
			ch := RunChildProcess(ctx, nonexistentCmd, outWriter, errWriter, false, log)

			select {
			case <-time.After(nonexistentCmdTimeout):
				Fail("command timed out")

			case status, ok := <-ch:
				Expect(ok).To(BeTrue())
				Expect(status.Err).To(HaveOccurred())
				Expect(status.ExitCode).To(BeZero()) // no process so no exit so zero-value for exit code
			}
		})

		It("should pass child failure in channel", func() {
			ch := RunChildProcess(ctx, failCmd, outWriter, errWriter, false, log)

			select {
			case <-time.After(failCmdTimeout):
				Fail("command timed out")

			case status, ok := <-ch:
				Expect(ok).To(BeTrue())
				Expect(status.Err).To(HaveOccurred())
				Expect(status.ExitCode).To(BeEquivalentTo(failCmdExitCode))
			}
		})

		It("should respond to context cancellation", func() {
			ctx, cancel := context.WithCancel(ctx)
			defer cancel()

			ch := RunChildProcess(ctx, sleepCmd, outWriter, errWriter, false, log)
			cancel()

			select {
			case <-time.After(sleepCmdTimeout):
				Fail("command timed out")

			case status, ok := <-ch:
				Expect(ok).To(BeTrue())
				Expect(status.Err).To(BeAssignableToTypeOf(&exec.ExitError{}))
				Expect(status.ExitCode).To(BeEquivalentTo(-1))
			}
		})

		toMapList := func(buf string) []map[string]interface{} {
			var ret []map[string]interface{}

			for _, s := range strings.Split(buf, "\n") {
				if len(s) == 0 {
					continue
				}
				line := make(map[string]interface{})
				err := json.Unmarshal([]byte(s), &line)
				Expect(err).NotTo(HaveOccurred())

				ret = append(ret, line)
			}

			return ret
		}

		includeMessageAtLevel := func(message, level string) OmegaMatcher {
			return WithTransform(
				toMapList,
				ContainElement(
					And(
						HaveKeyWithValue("level", level),
						HaveKeyWithValue("msg", message),
					)))
		}

		Context("Without Log Wrapping", func() {
			It("should relay child process logs", func() {
				ch := RunChildProcess(ctx, outputCmd, outWriter, errWriter, false, log)

				select {
				case <-time.After(outputCmdTimeout):
					Fail("command timed out")

				case <-ch:
					Expect(outWriter.String()).To(Equal(outMessage + "\n"))
					Expect(errWriter.String()).To(Equal(errMessage + "\n"))

					Expect(logOutWriter.String()).NotTo(includeMessageAtLevel(outMessage, "info"))
					Expect(logOutWriter.String()).NotTo(includeMessageAtLevel(errMessage, "error"))
				}
			})
		})

		Context("With Log Wrapping", func() {
			It("should relay child process logs", func() {
				ch := RunChildProcess(ctx, outputCmd, outWriter, errWriter, true, log)

				select {
				case <-time.After(outputCmdTimeout):
					Fail("command timed out")

				case <-ch:
					logs := logOutWriter.String()

					Expect(logs).To(includeMessageAtLevel(outMessage, "info"))
					Expect(logs).To(includeMessageAtLevel(errMessage, "error"))

					Expect(outWriter.String()).To(BeEmpty())
					Expect(errWriter.String()).To(BeEmpty())
				}
			})
		})
	})
})
