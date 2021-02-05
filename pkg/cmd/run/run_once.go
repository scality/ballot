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

	"github.com/scality/ballot/pkg/runengine"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var onceCmd = cobra.Command{
	Use:     "once",
	Example: "ballot run once --candidate-id `hostname`-$$ -- /usr/bin/env LD_PRELOAD=trickle.so caddy file-server -browse --listen :8000",
	Short:   "Runs a command after acquiring leadership from a ZooKeeper cluster",
	Args:    cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		ctx := context.Background()
		rootLog := log.StandardLogger().WithContext(ctx).WithField("name", "cmd-run-once")

		runCommon(ctx, cmd, args, runengine.RunAsLeader, rootLog)
	},
}

func AddOnce(parent *cobra.Command) {
	parent.AddCommand(&onceCmd)
}
