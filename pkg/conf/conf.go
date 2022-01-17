// Copyright 2021-2022 Scality, Inc
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

package conf

import (
	"errors"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	flagCandidateID             = "candidate-id"
	flagOnElectionFailure       = "on-election-failure"
	flagOnFailure               = "on-child-error"
	flagOnSuccess               = "on-child-success"
	flagSchedule                = "schedule"
	flagWrapChildLogs           = "wrap-child-logs"
	flagZooKeeperServers        = "zookeeper-servers"
	flagZooKeeperBasePath       = "zookeeper-base-path"
	flagZooKeeperSessionTimeout = "zookeeper-session-timeout"
	flagDebugMode               = "debug"
	flagOutputFormat            = "output-format"
)

var defaultZooKeeperSessionTimeout = 5 * time.Second

func AddZooKeeperFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String(flagZooKeeperServers, "localhost", "ZooKeeper servers")
	cmd.PersistentFlags().String(flagZooKeeperBasePath, "/ballot/election", "ZooKeeper base path")
	cmd.PersistentFlags().Duration(flagZooKeeperSessionTimeout, defaultZooKeeperSessionTimeout, "ZooKeeper session timeout")

	viper.BindPFlags(cmd.PersistentFlags())
}

func GetZooKeeperServers() []string {
	return viper.GetStringSlice(flagZooKeeperServers)
}

func GetZooKeeperBasePath() string {
	return viper.GetString(flagZooKeeperBasePath)
}

func GetZooKeeperSessionTimeout() time.Duration {
	return viper.GetDuration(flagZooKeeperSessionTimeout)
}

func GetDebugMode() bool {
	return viper.GetBool(flagDebugMode)
}

func GetOutputFormat() string {
	return viper.GetString(flagOutputFormat)
}

var errInvalidOnElectionFailureFlag = errors.New("invalid value for " + flagOnElectionFailure)
var errInvalidOnFailureFlag = errors.New("invalid value for " + flagOnFailure)
var errInvalidOnSuccessFlag = errors.New("invalid value for " + flagOnSuccess)

type TerminationPolicy int

const (
	TerminationPolicyReelect TerminationPolicy = iota
	TerminationPolicyRerun
	TerminationPolicyExit
	TerminationPolicyIgnore
)

type ElectionFailurePolicy int

const (
	ElectionFailurePolicyRetry ElectionFailurePolicy = iota
	ElectionFailurePolicyRunAnyway
	ElectionFailurePolicyExit
)

type RunParams struct {
	CandidateID string

	Schedule          string
	OnSuccess         TerminationPolicy
	OnFailure         TerminationPolicy
	OnElectionFailure ElectionFailurePolicy

	MultipleRuns bool
	WrapLogs     bool
	DebugMode    bool
}

func toTerminationPolicy(str string) (TerminationPolicy, error) {
	switch str {
	case "reelect":
		return TerminationPolicyReelect, nil
	case "rerun":
		return TerminationPolicyRerun, nil
	case "exit":
		return TerminationPolicyExit, nil
	case "ignore":
		return TerminationPolicyIgnore, nil
	}

	return 0, errInvalidOnSuccessFlag
}

func toElectionFailurePolicy(str string) (ElectionFailurePolicy, error) {
	switch str {
	case "retry":
		return ElectionFailurePolicyRetry, nil
	case "run-anyway":
		return ElectionFailurePolicyRunAnyway, nil
	case "exit":
		return ElectionFailurePolicyExit, nil
	}

	return 0, errInvalidOnSuccessFlag
}

func ParseRunParams(v *viper.Viper) (*RunParams, error) {
	stp, err := toTerminationPolicy(v.GetString(flagOnSuccess))
	if err != nil {
		return nil, errInvalidOnSuccessFlag
	}

	ftp, err := toTerminationPolicy(v.GetString(flagOnFailure))
	if err != nil {
		return nil, errInvalidOnFailureFlag
	}

	oef, err := toElectionFailurePolicy(v.GetString(flagOnElectionFailure))
	if err != nil {
		return nil, errInvalidOnElectionFailureFlag
	}

	params := &RunParams{
		WrapLogs:          v.GetBool(flagWrapChildLogs),
		OnSuccess:         stp,
		OnFailure:         ftp,
		OnElectionFailure: oef,
		CandidateID:       v.GetString(flagCandidateID),
		Schedule:          v.GetString(flagSchedule),
		MultipleRuns:      v.IsSet(flagSchedule),
		DebugMode:         GetDebugMode(),
	}

	return params, nil
}

func AddScheduleFlags(cmd *cobra.Command) {
	cmd.Flags().String(flagSchedule, "",
		"Schedule in extended cron format")
	cmd.MarkFlagRequired(flagSchedule)
}

func AddRunFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().Bool(flagWrapChildLogs, true, "Wrap command logs.")
	cmd.PersistentFlags().String(flagOnSuccess, "reelect",
		"What to do when the child process exits successfully.\nOne of \"rerun\", \"reelect\", \"exit\", \"ignore\".")
	cmd.PersistentFlags().String(flagOnFailure, "reelect",
		"What to do when the child process exits with an error code.\nOne of \"rerun\", \"reelect\", \"exit\", \"ignore\".")
	cmd.PersistentFlags().String(flagOnElectionFailure, "retry",
		"What to do when the election fails.\nOne of \"retry\", \"run-anyway\", \"exit\".")

	cmd.PersistentFlags().String(flagCandidateID, "", "Candidate id")
	cmd.MarkPersistentFlagRequired(flagCandidateID)

	viper.BindPFlags(cmd.PersistentFlags())
}

func AddInfoFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().StringP(flagOutputFormat, "o", "yaml", "Output format\nOne of \"yaml\", \"json\"")

	viper.BindPFlags(cmd.PersistentFlags())
}
