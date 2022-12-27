package cmd

import (
	"fmt"
	"os"

	"cuelang.org/go/cue/errors"
	"github.com/joomcode/errorx"
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
	if errx, ok := err.(*errorx.Error); ok && cue.CueErrors.IsNamespaceOf(errx.Type()) {
		fmt.Printf("Error in configuration: [%s] %s \n", errx.Type().FullName(), errx.Message())
		fmt.Println(errors.Details(errx.Cause(), nil))
		os.Exit(1)
	}
	if err != nil {
		return err
	}
	//err = podman.Execute(config);
	err = systemd.Create(*config)
	if err != nil {
		return err
	}
	return nil
}
