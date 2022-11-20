package cmd

import (
	"github.com/spf13/cobra"

	"yuri91/sloop/cue"
	//	"yuri91/sloop/podman"
	"yuri91/sloop/systemd"
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
	config, err := cue.GetConfig(".")
	if err != nil {
		return err
	}
	//err = podman.Execute(config);
	err = systemd.Create(config)
	if err != nil {
		return err
	}
	return nil
}
