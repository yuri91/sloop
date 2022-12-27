package cmd

import (
	"fmt"
	"os"

	"cuelang.org/go/cue/errors"
	"github.com/joomcode/errorx"
	"github.com/kr/pretty"
	"github.com/spf13/cobra"

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
	if errx, ok := err.(*errorx.Error); ok && cue.CueErrors.IsNamespaceOf(errx.Type()) {
		fmt.Printf("Error in configuration: [%s] %s \n", errx.Type().FullName(), errx.Message())
		fmt.Println(errors.Details(errx.Cause(), nil))
		os.Exit(1)
	}
	if err != nil {
		return err
	}
	fmt.Printf("%# v\n", pretty.Formatter(config));
	return nil
}
