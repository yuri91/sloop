package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"yuri91/sloop/cue"
	"yuri91/sloop/podman"
)

var (
	purgeCmd = &cobra.Command{
		Use:   "purge",
		Short: "Purge sloop containers and services",
		Long: `Purge sloop containers and services`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return purge()
		},
	}
)

func init() {
}

func purge() error {
	err := os.Chdir(confDir)
	if err != nil {
		return err
	}
	config, err := cue.GetConfig(".")
	if err != nil {
		return err
	}
	err = podman.Purge(config);
	if err != nil {
		return err
	}
	return nil
}
