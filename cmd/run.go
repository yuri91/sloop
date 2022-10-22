package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"yuri91/sloop/cue"
	"yuri91/sloop/podman"
)

var (
	runCmd = &cobra.Command{
		Use:   "run",
		Short: "Run the cue configuration",
		Long: `Run the cue configuration`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return run()
		},
	}
)

func init() {
}

func run() error {
	err := os.Chdir(confDir)
	if err != nil {
		return err
	}
	config, err := cue.GetConfig(".")
	if err != nil {
		return err
	}
	err = podman.Execute(config);
	if err != nil {
		return err
	}
	return nil
}
