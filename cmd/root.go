/*
Copyright © 2020 NAME HERE <EMAIL ADDRESS>

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/
package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/jpiriz/ghcontrib/internal"
	"github.com/jpiriz/ghcontrib/pkg/cache"
	"github.com/jpiriz/ghcontrib/pkg/githubclient"
	"github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
)

var githubToken string
var cacheAddr string
var cacheDb int
var cachePassword string
var listenAddr string
var verbose bool

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "ghcontrib",
	Short: "Application to get top github contributors by City",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		logrus.SetOutput(os.Stdout)
		loglevel := "INFO"
		if verbose {
			loglevel = "DEBUG"
		}
		lvl, err := logrus.ParseLevel(loglevel)
		if err != nil {
			return err
		}
		logrus.SetLevel(lvl)
		return nil
	},
	Run: func(cmd *cobra.Command, args []string) {
		logrus.Info("Starting GH-Contrib API")
		var ctx = context.Background()
		cache := cache.NewRedisCache(cacheAddr, cachePassword)
		ghClient := githubclient.NewClient(ctx, githubToken)
		app := internal.NewApp(listenAddr, ghClient, cache)
		//app := internal.NewApp(listenAddr, githubToken)
		app.StartServer()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().StringVar(&githubToken, "github_token", "", "Token for Github Api")
	rootCmd.PersistentFlags().StringVar(&listenAddr, "listen_addr", ":10000", "Address where the service should listen")
	rootCmd.PersistentFlags().StringVar(&cacheAddr, "cache_addr", "localhost:6379", "Cache Host:Port to connect to")
	rootCmd.PersistentFlags().StringVar(&cachePassword, "cache_password", "", "Cache password")
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "show debug information")
}
