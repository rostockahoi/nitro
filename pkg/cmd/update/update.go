package update

import (
	"fmt"

	"github.com/craftcms/nitro/pkg/client"
	"github.com/spf13/cobra"
)

// UpdateCommand is the command for creating new development environments
var UpdateCommand = &cobra.Command{
	Use:   "update",
	Short: "Update Docker images",
	RunE:  updateMain,
	Example: `  # update docker images
  nitro update`,
}

func updateMain(cmd *cobra.Command, args []string) error {
	env := cmd.Flag("environment").Value.String()

	// create the new client
	nitro, err := client.NewClient()
	if err != nil {
		return fmt.Errorf("unable to create a client for docker, %w", err)
	}

	images := []string{"docker.io/craftcms/php-fpm:7.4-dev", "docker.io/craftcms/php-fpm:7.3-dev"}

	if err := nitro.Update(cmd.Context(), images); err != nil {
		return err
	}

	if cmd.Flag("restart").Value.String() == "true" {
		return nitro.Restart(cmd.Context(), env, args)
	}

	return nil
}

func init() {
	flags := UpdateCommand.Flags()

	flags.BoolP("restart", "r", true, "restart containers after update")
}