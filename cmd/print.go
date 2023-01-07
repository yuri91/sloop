package cmd

import (
	"fmt"
	"os"

	"cuelang.org/go/cue/errors"
	"github.com/joomcode/errorx"
	"github.com/spf13/cobra"

	"yuri91/sloop/cue"
)

var (
	printCmd = &cobra.Command{
		Use:   "print",
		Short: "Print part of the cue configuration",
		Long: `Print part of the cue configuration, by specifying a path`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return print(args[0])
		},
	}
)

func init() {
}

func print(path string) error {
	value, err := cue.GetCueConfig(".")
	if errx, ok := err.(*errorx.Error); ok && cue.CueErrors.IsNamespaceOf(errx.Type()) {
		fmt.Printf("Error in configuration: [%s] %s \n", errx.Type().FullName(), errx.Message())
		fmt.Println(errors.Details(errx.Cause(), nil))
		os.Exit(1)
	}
	if err != nil {
		return err
	}
	cue.Print(*value, path)
	return nil
}
