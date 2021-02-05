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

package run

import (
	"context"
	"os"

	"github.com/scality/ballot/pkg/conf"
	"github.com/scality/ballot/pkg/election"
	"github.com/scality/ballot/pkg/process"
	"github.com/scality/ballot/pkg/runengine"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func runCommon(ctx context.Context, cmd *cobra.Command, args []string, leaderRunner runengine.LeaderRunner, l *logrus.Entry) {
	params, err := conf.ParseRunParams(viper.GetViper())
	if err != nil {
		l.Error(err)
		cmd.Help()

		return
	}

	el, err := election.NewZooKeeperElection(
		conf.GetZooKeeperServers(),
		conf.GetZooKeeperBasePath(),
		params.CandidateID,
		conf.GetZooKeeperSessionTimeout(),
		params.DebugMode,
		l.WithField("name", "election"),
	)
	if err != nil {
		l.Fatal(err)

		return
	}

	runner := func() <-chan process.RunStatus {
		return process.RunChildProcess(ctx, args, cmd.OutOrStdout(), cmd.ErrOrStderr(), params.WrapLogs, l)
	}

	runStatus := leaderRunner(ctx, el, runner, params, l)
	if runStatus.Err != nil {
		l.Error(runStatus.Err)
	}

	os.Exit(runStatus.ExitCode)
}
