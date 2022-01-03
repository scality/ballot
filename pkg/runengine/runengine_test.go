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
	"bytes"
	"context"
	"errors"
	"strings"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/ginkgo/extensions/table"
	. "github.com/onsi/gomega"
	"github.com/scality/ballot/pkg/conf"
	"github.com/scality/ballot/pkg/process"
	"github.com/sirupsen/logrus"
)

var errTestElection = errors.New("election error")
var errTestProcess = errors.New("process error")

func newErrorMockElection() *mockElection {
	return &mockElection{
		returnForBecomeLeader: []error{
			errTestElection,
		},
	}
}

func new2ErrorsMockElection() *mockElection {
	return &mockElection{
		returnForBecomeLeader: []error{
			errTestElection,
			errTestElection,
			nil,
		},
	}
}

type runnerSuite struct {
	name     string
	runner   LeaderRunner
	schedule string
}

type testCase struct {
	runner                LeaderRunner
	election              *mockElection
	runParams             conf.RunParams
	schedule              string
	processExitCode       int
	processErrors         []error
	expectElectionTries   int
	expectResignations    int
	expectReinits         int
	expectProcessRunCount int
	expectExitCode        int
	expectError           error
}

var runParamsElectionFailureRunAnyway = conf.RunParams{
	OnElectionFailure: conf.ElectionFailurePolicyRunAnyway,
	OnSuccess:         conf.TerminationPolicyExit,
	OnFailure:         conf.TerminationPolicyExit,
}

var runParamsElectionFailureExit = conf.RunParams{
	OnElectionFailure: conf.ElectionFailurePolicyExit,
	OnSuccess:         conf.TerminationPolicyExit,
	OnFailure:         conf.TerminationPolicyExit,
}

var runParamsElectionFailureRetry = conf.RunParams{
	OnElectionFailure: conf.ElectionFailurePolicyRetry,
	OnSuccess:         conf.TerminationPolicyExit,
	OnFailure:         conf.TerminationPolicyExit,
}

var _ = Describe("Runengine", func() {
	failureExitCode := 123

	var logs *bytes.Buffer
	logger := logrus.New()
	logger.SetLevel(logrus.TraceLevel)
	logger.ReportCaller = true
	l := logrus.NewEntry(logger)

	BeforeEach(func() {
		logs = bytes.NewBuffer(nil)
		logger.Out = logs
	})

	AfterEach(func() {
		t := GinkgoT()
		if t.Failed() && logs.Len() > 0 {
			t.Log("Logs for test failure [", t.Name(), "]:")
			for _, l := range strings.Split(logs.String(), "\n") {
				t.Log(l)
			}
		}
	})

	testRunnerInvocation := func(t testCase) {
		calledCount := 0

		exec := func() <-chan process.RunStatus {
			var err error

			if len(t.processErrors) > 0 {
				err = t.processErrors[calledCount]
			}

			calledCount++
			r := make(chan process.RunStatus, 1)
			r <- process.RunStatus{
				ExitCode: t.processExitCode,
				Err:      err,
			}

			return r
		}

		params := t.runParams
		params.Schedule = t.schedule

		ctx := context.Background()
		ret := t.runner(ctx, t.election, exec, &params, l)

		Expect(ret.ExitCode).To(Equal(t.expectExitCode))
		Expect(t.election.becomeLeaderCalledNTimes).To(Equal(t.expectElectionTries))
		Expect(t.election.calledResignNTimes).To(Equal(t.expectResignations))
		Expect(t.election.calledReinitNTimes).To(Equal(t.expectReinits))
		Expect(calledCount).To(Equal(t.expectProcessRunCount))

		if t.expectError != nil {
			Expect(ret.Err).To(MatchError(t.expectError))
		} else {
			Expect(ret.Err).NotTo(HaveOccurred())
		}
	}

	runnersToTest := []runnerSuite{
		{
			name:   "RunAsLeader",
			runner: RunAsLeader,
		},
		{
			name:     "RunAsLeaderWithSchedule",
			runner:   RunAsLeaderWithSchedule,
			schedule: "@every 1s",
		},
	}

	for _, suite := range runnersToTest {
		suite := suite

		Describe(suite.name, func() {
			DescribeTable(
				"Election Failure Policy",
				testRunnerInvocation,
				Entry(
					"`run-anyway` should run the process and return its exit code",
					testCase{
						runner:                suite.runner,
						election:              newErrorMockElection(),
						runParams:             runParamsElectionFailureRunAnyway,
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 1,
					},
				),
				Entry(
					"`exit` should return an error and exit code 1 without running process",
					testCase{
						runner:                suite.runner,
						election:              newErrorMockElection(),
						runParams:             runParamsElectionFailureExit,
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectExitCode:        1,
						expectError:           errTestElection,
						expectProcessRunCount: 0,
					},
				),
				Entry(
					"`retry` should retry to become leader until successful",
					testCase{
						runner:                suite.runner,
						election:              new2ErrorsMockElection(),
						runParams:             runParamsElectionFailureRetry,
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   3,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 1,
						// 2 errors in returnForBecomeLeader means 2 reinits as it has to re-try the election after receiving each error
						expectReinits: 2,
					},
				),
			)

			DescribeTable(
				"Election Success Policy",
				testRunnerInvocation,
				Entry(
					"`run-anyway` should run the process and return its exit code",
					testCase{
						runner:                suite.runner,
						election:              &mockElection{},
						runParams:             runParamsElectionFailureRunAnyway,
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 1,
					},
				),
				Entry(
					"`exit` should run the process and return its exit code",
					testCase{
						runner:                suite.runner,
						election:              &mockElection{},
						runParams:             runParamsElectionFailureExit,
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 1,
					},
				),
				Entry(
					"`retry` should run the process and return its exit code",
					testCase{
						runner:                suite.runner,
						election:              &mockElection{},
						runParams:             runParamsElectionFailureRetry,
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 1,
					},
				),
			)

			DescribeTable(
				"Process Failure Policy",
				testRunnerInvocation,
				Entry(
					"`exit` should run the process and return its exit code",
					testCase{
						runner:   suite.runner,
						election: &mockElection{},
						runParams: conf.RunParams{
							OnFailure: conf.TerminationPolicyExit,
							OnSuccess: conf.TerminationPolicyExit,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						processErrors:         []error{errTestProcess},
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectError:           errTestProcess,
						expectProcessRunCount: 1,
					},
				),
				Entry(
					"`reelect` should run the process then re-run the election",
					testCase{
						runner: suite.runner,
						election: &mockElection{
							returnForBecomeLeader: []error{nil, errTestElection},
						},
						runParams: conf.RunParams{
							OnElectionFailure: conf.ElectionFailurePolicyExit,
							OnFailure:         conf.TerminationPolicyReelect,
							OnSuccess:         conf.TerminationPolicyExit,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						processErrors:         []error{errTestProcess},
						expectElectionTries:   2,
						expectResignations:    1,
						expectExitCode:        1,
						expectError:           errTestElection,
						expectProcessRunCount: 1,
						// 1 error in returnForBecomeLeader means 1 reinit as it has to re-try the election after receiving each error
						expectReinits: 1,
					},
				),
				Entry(
					"`rerun` should run the process again",
					testCase{
						runner:   suite.runner,
						election: &mockElection{},
						runParams: conf.RunParams{
							OnElectionFailure: conf.ElectionFailurePolicyExit,
							OnFailure:         conf.TerminationPolicyRerun,
							OnSuccess:         conf.TerminationPolicyExit,
						},
						schedule:        suite.schedule,
						processExitCode: failureExitCode,
						processErrors: []error{
							errTestProcess,
							errTestProcess,
							nil,
						},
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 3,
					},
				),
				Entry(
					"`ignore` should run the process and return success anyway",
					testCase{
						runner:   suite.runner,
						election: &mockElection{},
						runParams: conf.RunParams{
							OnElectionFailure: conf.ElectionFailurePolicyExit,
							OnFailure:         conf.TerminationPolicyIgnore,
							OnSuccess:         conf.TerminationPolicyExit,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						processErrors:         []error{errTestProcess},
						expectElectionTries:   1,
						expectProcessRunCount: 1,
					},
				),
				Entry(
					"`reelect` with error when resigning should return immediately",
					testCase{
						runner: suite.runner,
						election: &mockElection{
							returnForResign: []error{errTestElection},
						},
						runParams: conf.RunParams{
							OnSuccess: conf.TerminationPolicyReelect,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						processErrors:         []error{errTestProcess},
						expectElectionTries:   1,
						expectResignations:    1,
						expectExitCode:        failureExitCode,
						expectError:           errTestProcess,
						expectProcessRunCount: 1,
					},
				),
			)

			DescribeTable(
				"Process Normal Termination Policy",
				testRunnerInvocation,
				Entry(
					"`exit` should run the process and return its exit code",
					testCase{
						runner:   suite.runner,
						election: &mockElection{},
						runParams: conf.RunParams{
							OnSuccess: conf.TerminationPolicyExit,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectProcessRunCount: 1,
						expectExitCode:        failureExitCode,
					},
				),
				Entry(
					"`reelect` should run the process then re-run the election",
					testCase{
						runner: suite.runner,
						election: &mockElection{
							returnForBecomeLeader: []error{nil, errTestElection},
						},
						runParams: conf.RunParams{
							OnElectionFailure: conf.ElectionFailurePolicyExit,
							OnSuccess:         conf.TerminationPolicyReelect,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   2,
						expectResignations:    1,
						expectExitCode:        1,
						expectError:           errTestElection,
						expectProcessRunCount: 1,
						// 1 error in returnForBecomeLeader means 1 reinit as it has to re-try the election after receiving each error
						expectReinits: 1,
					},
				),
				Entry(
					"`rerun` should run the process again",
					testCase{
						runner:   suite.runner,
						election: &mockElection{},
						runParams: conf.RunParams{
							OnSuccess: conf.TerminationPolicyRerun,
							OnFailure: conf.TerminationPolicyExit,
						},
						schedule:        suite.schedule,
						processExitCode: failureExitCode,
						processErrors: []error{
							nil,
							nil,
							errTestProcess,
						},
						expectElectionTries:   1,
						expectExitCode:        failureExitCode,
						expectProcessRunCount: 3,
						expectError:           errTestProcess,
					},
				),
				Entry(
					"`ignore` should run the process and return success",
					testCase{
						runner:   suite.runner,
						election: &mockElection{},
						runParams: conf.RunParams{
							OnSuccess: conf.TerminationPolicyIgnore,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectProcessRunCount: 1,
					},
				),
				Entry(
					"`reelect` with error when resigning should return immediately",
					testCase{
						runner: suite.runner,
						election: &mockElection{
							returnForResign: []error{errTestElection},
						},
						runParams: conf.RunParams{
							OnSuccess: conf.TerminationPolicyReelect,
						},
						schedule:              suite.schedule,
						processExitCode:       failureExitCode,
						expectElectionTries:   1,
						expectResignations:    1,
						expectExitCode:        failureExitCode,
						expectError:           errTestElection,
						expectProcessRunCount: 1,
					},
				),
			)
		})
	}
})

type mockElection struct {
	returnForBecomeLeader    []error
	becomeLeaderCalledNTimes int

	returnForResign    []error
	calledResignNTimes int

	calledReinitNTimes int
}

func (me *mockElection) BecomeLeader(ctx context.Context) error {
	n := me.becomeLeaderCalledNTimes
	me.becomeLeaderCalledNTimes++

	if me.returnForBecomeLeader == nil {
		return nil
	}

	return me.returnForBecomeLeader[n]
}

func (me *mockElection) Resign(ctx context.Context) error {
	n := me.calledResignNTimes
	me.calledResignNTimes++

	if me.returnForResign == nil {
		return nil
	}

	return me.returnForResign[n]
}

func (me *mockElection) Reinit() error {
	me.calledReinitNTimes++

	return nil
}
