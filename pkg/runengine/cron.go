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

package runengine

import (
	"context"
	"fmt"
	"sync"

	"github.com/robfig/cron/v3"
	"github.com/scality/ballot/pkg/conf"
	"github.com/scality/ballot/pkg/election"
	"github.com/scality/ballot/pkg/process"
	"github.com/sirupsen/logrus"
)

func runAsLeaderWithSchedule(ctx context.Context, el election.Election, exec process.ExecuteFunc, params *conf.RunParams, l *logrus.Entry, leaderRunner LeaderRunner) process.RunStatus {
	c := cron.New()

	runMutex := sync.Mutex{}
	running := false
	rLog := l.WithField("name", "cmd-run-cron-runner")

	runCronStatusChan := make(chan process.RunStatus)

	_, err := c.AddFunc(params.Schedule, func() {
		runMutex.Lock()
		if running {
			rLog.Info("already running, skipping")
			runMutex.Unlock()

			return
		}
		running = true
		runMutex.Unlock()

		status, ok := <-exec()
		if !ok {
			close(runCronStatusChan)
		} else {
			runCronStatusChan <- status
		}

		runMutex.Lock()
		running = false
		runMutex.Unlock()
	})
	if err != nil {
		return process.RunStatus{
			ExitCode: 1,
			Err:      fmt.Errorf("cron set up: %w", err),
		}
	}

	// this is the function that:
	// - waits for commands to finish and forwards status from the cron goroutine to the RunAsLeader goroutine
	// - notices context cancelation
	// - is called again when `RunAsLeader` wants to retry
	// - starts the cron scheduler and stops it when returning
	executor := func() <-chan process.RunStatus {
		runAsLeaderStatusChan := make(chan process.RunStatus)

		go func() {
			defer close(runAsLeaderStatusChan)

			c.Start()

			defer func() {
				<-c.Stop().Done()
			}()

			for {
				select {
				case <-ctx.Done():
					return
				case s, ok := <-runCronStatusChan:
					if ok {
						runAsLeaderStatusChan <- s
					}

					return
				}
			}
		}()

		return runAsLeaderStatusChan
	}

	return leaderRunner(ctx, el, executor, params, l)
}

func RunAsLeaderWithSchedule(ctx context.Context, el election.Election, exec process.ExecuteFunc, params *conf.RunParams, l *logrus.Entry) process.RunStatus {
	return runAsLeaderWithSchedule(ctx, el, exec, params, l, RunAsLeader)
}

var _ LeaderRunner = RunAsLeaderWithSchedule
