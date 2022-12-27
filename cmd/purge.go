package cmd

import (
	"github.com/spf13/cobra"

	"yuri91/sloop/systemd"
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
var purgeImages bool
func init() {
	purgeCmd.PersistentFlags().BoolVarP(&purgeImages, "images", "i", false, "also purge cached images")
}

func purge() error {
	err := systemd.Purge(purgeImages);
	if err != nil {
		return err
	}
	return nil
}
