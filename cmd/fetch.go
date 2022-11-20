package cmd

import (
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"yuri91/sloop/common"
	"yuri91/sloop/image"
)

var (
	fetchCmd = &cobra.Command{
		Use:   "fetch",
		Short: "Fetch required images",
		Long: `Fetch required images`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return fetch(args[0], args[1])
		},
	}
)

func init() {
}

func fetch(imageName string, tag string) error {
	bundlePath := filepath.Join(common.ImagePath, imageName)
	if _, err := os.Stat(bundlePath); os.IsNotExist(err) {
		return image.Fetch(imageName, tag, bundlePath)
	}
	return nil
}
