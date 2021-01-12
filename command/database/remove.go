package database

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"

	"github.com/craftcms/nitro/pkg/backup"
	"github.com/craftcms/nitro/pkg/labels"
	"github.com/craftcms/nitro/pkg/terminal"
)

var removeExampleText = `  # remove a database
  nitro db remove`

func removeCommand(docker client.CommonAPIClient, output terminal.Outputer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "remove",
		Short:   "Remove a database",
		Example: removeExampleText,
		RunE: func(cmd *cobra.Command, args []string) error {
			show, err := strconv.ParseBool(cmd.Flag("show-output").Value.String())
			if err != nil {
				// set to false
				show = false
			}

			// add filters to show only the environment and database containers
			filter := filters.NewArgs()
			filter.Add("label", labels.Nitro)
			filter.Add("label", labels.Type+"=database")

			// get a list of all the databases
			containers, err := docker.ContainerList(cmd.Context(), types.ContainerListOptions{Filters: filter})
			if err != nil {
				return err
			}

			// sort containers by the name
			sort.SliceStable(containers, func(i, j int) bool {
				return containers[i].Names[0] < containers[j].Names[0]
			})

			var containerList []string
			for _, c := range containers {
				containerList = append(containerList, strings.TrimLeft(c.Names[0], "/"))
			}

			// get the container id, name, and database from the user
			containerID, _, compatibility, db, err := backup.Prompt(cmd.Context(), os.Stdin, docker, output, containers, containerList)
			if err != nil {
				return err
			}

			// ask the user for the database to create
			msg := "Enter the new database to drop: "

			fmt.Print(msg)
			for {
				rdr := bufio.NewReader(os.Stdin)
				input, err := rdr.ReadString('\n')
				if err != nil {
					return err
				}

				if !strings.ContainsAny(input, " -") {
					db = strings.TrimSpace(input)
					break
				}

				fmt.Println("  no spaces or hyphens are allowed…")
				fmt.Print(msg)
			}

			output.Pending("creating database", db)

			// set the commands based on the engine type
			var cmds []string
			switch compatibility {
			case "mysql":
				cmds = []string{"mysqladmin", "-user=root", "-pnitro", "drop", db}
			default:
				cmds = []string{"psql", "--username=nitro", "--host=127.0.0.1", fmt.Sprintf(`-c "DROP DATABASE IF EXISTS %s;"`, db)}
			}

			// execute the command to create the database
			if _, err := execCreate(cmd.Context(), docker, containerID, cmds, show); err != nil {
				return err
			}

			output.Done()

			output.Info("Database removed 💪")

			return nil
		},
	}

	cmd.Flags().Bool("show-output", false, "show debug from import")

	return cmd
}
