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

package info

import (
	"context"
	"encoding/json"
	"os"

	"github.com/scality/ballot/pkg/conf"
	"github.com/scality/ballot/pkg/election"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

func runInfo(ctx context.Context, cmd *cobra.Command, args []string, appLogger *log.Entry) {
	electionClient, err := election.NewZooKeeperElectionClient(
		conf.GetZooKeeperServers(),
		conf.GetZooKeeperBasePath(),
		conf.GetZooKeeperSessionTimeout(),
		conf.GetDebugMode(),
		appLogger.WithField("name", "election-client"),
	)
	if err != nil {
		appLogger.Fatal(err)

		return
	}

	status, err := electionClient.Inspect()
	if err != nil {
		appLogger.Fatal(err)
	}

	format := conf.GetOutputFormat()
	switch format {
	case "yaml":
		enc := yaml.NewEncoder(os.Stdout)
		_ = enc.Encode(status)

	case "json":
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "    ")
		_ = enc.Encode(status)

	default:
		appLogger.Fatalf("invalid output format '%s'", format)
	}
}

var infoCmd = cobra.Command{
	Use:   "info",
	Short: "List candidates under a ZooKeeper path",
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		rootLog := log.StandardLogger().WithContext(ctx).WithField("name", "cmd-info")

		runInfo(ctx, cmd, args, rootLog)
	},
}

func Add(parent *cobra.Command) {
	conf.AddInfoFlags(&infoCmd)

	parent.AddCommand(&infoCmd)
}
