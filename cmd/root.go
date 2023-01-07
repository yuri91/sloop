package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"yuri91/sloop/common"

	"github.com/spf13/cobra"
)

var (
	confDir     string

	rootCmd = &cobra.Command{
		Use:   "sloop",
		Short: "A container generator and configurator",
		Long: `Sloop generates systemd units for running docker containers, without docker`,
		SilenceUsage: true,
		SilenceErrors: true,
	}
)

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Printf("Error: %+v", err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().StringVar(&confDir, "conf", ".", "configuration root directory")

	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(printCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(purgeCmd)
	rootCmd.AddCommand(initCmd)
	rootCmd.AddCommand(fetchCmd)
}

func initConfig() {
	confDir, err := filepath.Abs(confDir)
	if err != nil {
		cobra.CheckErr(err)
	}
	common.SetPaths(confDir)
	err = os.Chdir(confDir)
	if err != nil {
		cobra.CheckErr(err)
	}
}
