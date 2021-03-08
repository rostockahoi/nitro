package npm

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"

	"github.com/craftcms/nitro/pkg/config"
	"github.com/craftcms/nitro/pkg/labels"
	"github.com/craftcms/nitro/pkg/terminal"
)

var (
	// ErrNoPackageFile is returned when there is no package.json or package-lock.json file in a directory
	ErrNoPackageFile = fmt.Errorf("no package.json or package-lock.json was found")
)

const exampleText = `  # run npm install in a current directory
  nitro npm install

  # run npm update
  nitro npm update

  # run a script
  nitro npm run dev`

// NewCommand is the command used to run npm commands in a sites container.
func NewCommand(home string, docker client.CommonAPIClient, output terminal.Outputer) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "npm",
		Short:              "Run npm commands",
		Example:            exampleText,
		DisableFlagParsing: true,
		Args:               cobra.MinimumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			// is the docker api alive?
			if _, err := docker.Ping(cmd.Context()); err != nil {
				return fmt.Errorf("Couldn’t connect to Docker; please make sure Docker is running.")
			}

			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Root().Context()
			// get the current working directory
			wd, err := os.Getwd()
			if err != nil {
				return err
			}

			// load the config
			cfg, err := config.Load(home)
			if err != nil {
				return err
			}

			// create a filter for the environment
			filter := filters.NewArgs()
			filter.Add("label", labels.Nitro)

			// get a context aware list of sites
			sites := cfg.ListOfSitesByDirectory(home, wd)

			// create the options for the sites
			var options []string
			for _, s := range sites {
				options = append(options, s.Hostname)
			}

			// if there are found sites we want to show or connect to the first one, otherwise prompt for
			// which site to connect to.
			var site config.Site
			switch len(sites) {
			case 0:
				// prompt for the site to ssh into
				selected, err := output.Select(cmd.InOrStdin(), "Select a site: ", options)
				if err != nil {
					return err
				}

				// add the label to get the site
				filter.Add("label", labels.Host+"="+sites[selected].Hostname)
				site = sites[selected]
			case 1:
				output.Info("connecting to", sites[0].Hostname)

				// add the label to get the site
				filter.Add("label", labels.Host+"="+sites[0].Hostname)
				site = sites[0]
			default:
				// prompt for the site to ssh into
				selected, err := output.Select(cmd.InOrStdin(), "Select a site: ", options)
				if err != nil {
					return err
				}

				// add the label to get the site
				filter.Add("label", labels.Host+"="+sites[selected].Hostname)
				site = sites[selected]
			}

			// find the containers but limited to the site label
			containers, err := docker.ContainerList(ctx, types.ContainerListOptions{Filters: filter, All: true})
			if err != nil {
				return err
			}

			// are there any containers??
			if len(containers) == 0 {
				return fmt.Errorf("unable to find an matching site")
			}

			container := containers[0]

			// start the container if its not running
			if container.State != "running" {
				for _, command := range cmd.Root().Commands() {
					if command.Use == "start" {
						if err := command.RunE(cmd, []string{}); err != nil {
							return err
						}
					}
				}
			}

			// check if the node version is defined on the site
			if site.NodeVersion == "" {
				versions := []string{"14", "12", "10"}
				// prompt for the node version
				selected, err := output.Select(os.Stdin, "Choose a Node version: ", versions)
				if err != nil {
					return err
				}

				// set the node version for the site
				if err := cfg.SetSiteNodeVersion(site.Hostname, versions[selected]); err != nil {
					return err
				}

				// save the config file
				if err := cfg.Save(); err != nil {
					return err
				}

				// install the version of nodejs and npm, switch to apply?
				for _, command := range cmd.Root().Commands() {
					if command.Use == "apply" {
						if err := command.RunE(cmd, []string{}); err != nil {
							return err
						}
					}
				}
			}

			commands := []string{"exec", "-it", container.ID, "npm"}

			// get the container path
			path := site.GetContainerPath()
			if path != "" {
				commands = append(commands, fmt.Sprintf("%s/%s", path, "npm"))
			} else {
				commands = append(commands, "npm")
			}

			// append the provided args to the command
			commands = append(commands, args...)

			// find the docker executable
			cli, err := exec.LookPath("docker")
			if err != nil {
				return err
			}

			// create the command
			c := exec.Command(cli, commands...)

			c.Stdin = cmd.InOrStdin()
			c.Stderr = cmd.ErrOrStderr()
			c.Stdout = cmd.OutOrStdout()

			if err := c.Run(); err != nil {
				return err
			}

			return nil
		},
	}

	return cmd
}
