package cmd

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

var (
	initCmd = &cobra.Command{
		Use:   "init",
		Short: "Initialize a configuration directory",
		Long: `Initialize a configuration directory`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return doInit(args)
		},
	}
)

func init() {
}

func doInit(args []string) error {
	module := ""
	if len(args) > 0 {
		if len(args) != 1 {
			return fmt.Errorf("too many arguments")
		}
		module = args[0]
		u, err := url.Parse("https://" + module)
		if err != nil {
			return fmt.Errorf("invalid module name: %v", module)
		}
		if h := u.Hostname(); !strings.Contains(h, ".") {
			return fmt.Errorf("invalid host name %s", h)
		}
	}

	cwd, err := os.Getwd()
	if err != nil {
		return err
	}

	mod := filepath.Join(cwd, "cue.mod")

	_, err = os.Stat(mod)

	if err == nil {
		return fmt.Errorf("cue.mod directory already exists")
	}

	err = os.Mkdir(mod, 0755)
	if err != nil && !os.IsExist(err) {
		return err
	}

	f, err := os.Create(filepath.Join(mod, "module.cue"))
	if err != nil {
		return err
	}
	defer f.Close()

	// Set module even if it is empty, making it easier for users to fill it in.
	_, err = fmt.Fprintf(f, "module: %q\n", module)

	if err = os.Mkdir(filepath.Join(mod, "usr"), 0755); err != nil {
		return err
	}
	if err = os.Mkdir(filepath.Join(mod, "pkg"), 0755); err != nil {
		return err
	}

	return err
}
