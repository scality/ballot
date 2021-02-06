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

	"github.com/scality/ballot/pkg/conf"
	"github.com/scality/ballot/pkg/election"
	"github.com/scality/ballot/pkg/process"
	"github.com/sirupsen/logrus"
)

func RunAsLeader(ctx context.Context, el election.Election, exec process.ExecuteFunc, params *conf.RunParams, l *logrus.Entry) process.RunStatus {
	exitCode := 0
	var runStatusErr error

elect:
	for {
		l.Info("trying to become leader")

		err := el.BecomeLeader(ctx)
		if err != nil {
			l.Errorf("election failed: %v", err)

			switch params.OnElectionFailure {
			case conf.ElectionFailurePolicyRunAnyway:
				l.Info("running anyway")

			case conf.ElectionFailurePolicyExit:
				l.Info("exiting")

				exitCode = 1
				runStatusErr = err

				break elect

			case conf.ElectionFailurePolicyRetry:
				l.Info("retrying")

				continue elect
			}
		}

		l.Info("continuing as leader")

	run:
		for {
			runCh := exec()

			for {
				l.Debug("waiting for next event")

				select {
				case <-ctx.Done():
					return process.RunStatus{
						Err: ctx.Err(),
					}

				case status, ok := <-runCh:
					policy := params.OnSuccess

					if status.Err != nil || !ok {
						policy = params.OnFailure
						l.Error(status.Err)
					}

					switch policy {
					case conf.TerminationPolicyExit:
						l.Info("exiting")

						exitCode = status.ExitCode
						runStatusErr = status.Err

						break elect

					case conf.TerminationPolicyReelect:
						l.Info("re-electing")

						err := el.Resign(ctx)
						if err != nil {
							l.Errorf("could not resign: %v", err)
							l.Warn("exiting to avoid starving other candidates")

							exitCode = status.ExitCode
							runStatusErr = status.Err

							// Make sure we return an error even if the process
							// did not fail per se, as this will lead to an expected
							// exit. The error ensures the appropriate action is taken
							// by the parent process manager.
							if runStatusErr == nil {
								runStatusErr = err
							}

							break elect
						}

						continue elect

					case conf.TerminationPolicyRerun:
						l.Info("re-running")

						continue run

					case conf.TerminationPolicyIgnore:
						l.Info("ignoring")

						if params.MultipleRuns {
							continue run
						} else {
							break elect
						}
					}
				}
			}
		}
	}

	return process.RunStatus{
		ExitCode: exitCode,
		Err:      runStatusErr,
	}
}

var _ LeaderRunner = RunAsLeader
