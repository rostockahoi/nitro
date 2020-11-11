package initcmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/craftcms/nitro/pkg/client"
)

// InitCommand is the command for creating new development environments
var InitCommand = &cobra.Command{
	Use:   "init",
	Short: "Create environment",
	RunE:  initMain,
	Example: `  # create a new environment with the default name
  nitro init
  
  # create a new environment overriding the default name
  nitro init --environment my-new-env`,
}

func initMain(cmd *cobra.Command, args []string) error {
	env := cmd.Flag("environment").Value.String()

	// create the new client
	nitro, err := client.NewClient()
	if err != nil {
		return fmt.Errorf("unable to create a client for docker, %w", err)
	}

	return nitro.Init(cmd.Context(), env, args)
}