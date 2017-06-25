// Copyright © 2017 Kenny Freeman kenny.freeman@gmail.com
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package commands defines and implements command-line commands and flags
// used by prom2click.
package commands

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/s4z/prom2click/config"
	"github.com/s4z/prom2click/srv"
	"github.com/spf13/afero"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var cfgFile string

// P2CCmd is prom2clicks root command
var P2CCmd = &cobra.Command{
	Use:   "prom2click",
	Short: "prom2click is a remote storage adapter for Prometheus",
	Long:  `prom2click is a remote storage adapter for Prometheus`,
	RunE: func(cmd *cobra.Command, args []string) error {
		// load our configuration and defaults
		cfg, err := initConfig()
		if err != nil {
			fmt.Printf("Error loading configuration file %s: %s\n", cfgFile, err.Error())
			os.Exit(1)
		}

		// run an http server, listen for remote read/write requests
		srv, err := srv.NewServer(cfg)
		if err != nil {
			fmt.Printf("Error starting up: %s\n", err.Error())
			return err
		}
		err = srv.Run()
		srv.Shutdown()

		// all done, return error if any
		return err
	},
}

// Execute adds all child commands to the root command sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the P2CCmd.
func Execute() {
	AddCommands()

	if err := P2CCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

// AddCommands adds child commands to the root P2CCmd
func AddCommands() {
	P2CCmd.AddCommand(runCmd)
	return
}

func init() {
	//cobra.OnInitialize(initConfig)
	P2CCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.test.yaml)")
}

// initConfig reads in config file and ENV variables if set.
func initConfig() (*viper.Viper, error) {
	var (
		v      *viper.Viper
		err    error
		cfgDir string
	)

	if cfgFile != "" {
		cfgDir, err = filepath.Abs(cfgFile)
	} else {
		cfgDir, err = os.Getwd()
	}
	if err != nil {
		return v, err
	}

	viper.AutomaticEnv() // read in environment variables that match
	fs := afero.OsFs{}
	v, err = config.LoadConfig(fs, cfgDir, cfgFile)
	if err != nil {
		return v, err
	}
	return v, nil
}
