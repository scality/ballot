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

package main

import (
	"fmt"
	"os"
	"strings"
	"time"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/scality/ballot/pkg/cmd/info"
	"github.com/scality/ballot/pkg/cmd/run"
	"github.com/scality/ballot/pkg/cmd/watch"
	"github.com/scality/ballot/pkg/conf"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	flagDebug            = "debug"
	timestampFormat      = time.RFC3339Nano
	humanTimestampFormat = time.StampNano
)

var rootCmd = &cobra.Command{
	Use: os.Args[0],
	Run: func(cmd *cobra.Command, _ []string) {
		_ = cmd.Help()
		os.Exit(1)
	},
}

func initConfig() {
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	explicitConf := false

	if viper.IsSet("config-file") {
		viper.SetConfigFile(viper.GetString("config-file"))

		explicitConf = true
	} else {
		viper.AddConfigPath(".")
		h, err := homedir.Expand("~/.config/ballot")
		if err == nil {
			viper.AddConfigPath(h)
		}
		viper.AddConfigPath("/etc/ballot")
		viper.SetConfigName("ballot")
	}

	err := viper.ReadInConfig()
	if err != nil {
		if explicitConf {
			log.Fatal(fmt.Errorf("read configuration file: %w", err))
		} else {
			log.Debug(fmt.Errorf("read configuration file: %w", err))
		}
	}

	lvl, err := log.ParseLevel(viper.GetString("log-level"))
	if err != nil {
		log.Fatal(fmt.Errorf("set log level: %w", err))
	}

	log.SetLevel(lvl)

	var formatter log.Formatter

	f := viper.GetString("log-format")
	switch f {
	case "json":
		formatter = &log.JSONFormatter{
			TimestampFormat: timestampFormat,
		}

	case "human":
		formatter = &log.TextFormatter{
			QuoteEmptyFields: true,
			DisableSorting:   true,
			FullTimestamp:    true,
			TimestampFormat:  humanTimestampFormat,
		}

	case "raw":
		formatter = &log.TextFormatter{
			DisableColors:    true,
			ForceQuote:       true,
			FullTimestamp:    true,
			TimestampFormat:  timestampFormat,
			QuoteEmptyFields: true,
		}

	default:
		log.Fatalf("invalid log format: %s", f)
	}

	log.SetFormatter(formatter)
	log.SetReportCaller(viper.GetBool(flagDebug))
	log.SetOutput(os.Stdout)
}

func main() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().String("config-file", "ballot.yaml", "Configuration file to use (default search paths are {.,~/.config,/etc}/ballot/ballot.{yaml,json})")
	rootCmd.PersistentFlags().Bool(flagDebug, false, "Turn on debug mode")
	rootCmd.PersistentFlags().String("log-format", "human", `Log output format (one of "human", "json", "raw")`)

	allLevels := []string{}
	for _, l := range log.AllLevels {
		allLevels = append(allLevels, fmt.Sprintf(`"%s"`, l))
	}
	rootCmd.PersistentFlags().String("log-level", "info", "Log level (one of "+strings.Join(allLevels, ", ")+")")

	viper.BindPFlags(rootCmd.PersistentFlags())

	conf.AddZooKeeperFlags(rootCmd)

	run.Add(rootCmd)
	info.Add(rootCmd)
	watch.Add(rootCmd)

	if err := rootCmd.Execute(); err != nil {
		log.Fatal(err)
	}
}
