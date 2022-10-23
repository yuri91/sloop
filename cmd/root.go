package cmd

import (
	"github.com/spf13/cobra"
)

var (
	confDir     string

	rootCmd = &cobra.Command{
		Use:   "sloop",
		Short: "A container generator and configurator",
		Long: `Sloop generate images and containers, and systemd services for running them through podman`,
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringVar(&confDir, "conf", ".", "configuration root directory")

	rootCmd.AddCommand(checkCmd)
	rootCmd.AddCommand(runCmd)
	rootCmd.AddCommand(initCmd)
}
