package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/kr/pretty"

	"yuri91/sloop/cue"
)

var (
	checkCmd = &cobra.Command{
		Use:   "check",
		Short: "Check the cue configuration",
		Long: `Check the cue configuration, whithout actually applying it`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return check()
		},
	}
)

func init() {
}

func check() error {
	config, err := cue.GetConfig(".")
	if err != nil {
		return err
	}
	fmt.Printf("%# v\n", pretty.Formatter(config));
	return nil
}
