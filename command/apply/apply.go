package apply

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	volumetypes "github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/spf13/cobra"

	"github.com/craftcms/nitro/command/apply/internal/match"
	"github.com/craftcms/nitro/config"
	"github.com/craftcms/nitro/labels"
	"github.com/craftcms/nitro/protob"
	"github.com/craftcms/nitro/sudo"
	"github.com/craftcms/nitro/terminal"
)

var (
	// ErrNoNetwork is used when we cannot find the network
	ErrNoNetwork = fmt.Errorf("Unable to find the network")

	// NginxImage is the image used for sites, with the PHP version
	NginxImage = "docker.io/craftcms/nginx:%s-dev"

	// DatabaseImage is used for determining the engine and version
	DatabaseImage = "docker.io/library/%s:%s"
)

const exampleText = `  # apply changes from a config
  nitro apply`

// New takes a docker client and the terminal output to run the apply actions
func New(home string, docker client.CommonAPIClient, nitrod protob.NitroClient, output terminal.Outputer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "apply",
		Short:   "Apply changes to an environment",
		Example: exampleText,
		RunE: func(cmd *cobra.Command, args []string) error {
			env := cmd.Flag("environment").Value.String()
			ctx := cmd.Context()
			if ctx == nil {
				// when we call commands from other commands (e.g. init)
				// the context could be nil, so we set it to the parent
				// context just in case.
				ctx = cmd.Parent().Context()
			}

			// load the config
			cfg, err := config.Load(home, env)
			if err != nil {
				return err
			}

			// create a filter for the environment
			filter := filters.NewArgs()
			filter.Add("label", labels.Environment+"="+env)

			output.Info("Checking Network...")

			// find networks
			networks, err := docker.NetworkList(ctx, types.NetworkListOptions{Filters: filter})
			if err != nil {
				return fmt.Errorf("unable to list docker networks\n%w", err)
			}

			// get the network for the environment
			var networkID string
			for _, n := range networks {
				if n.Name == env {
					networkID = n.ID
					output.Success("network ready")
				}
			}

			// if the network is not found
			if networkID == "" {
				return ErrNoNetwork
			}

			output.Info("Checking Proxy...")

			proxyFilter := filters.NewArgs()
			proxyFilter.Add("label", labels.Proxy+"="+env)

			// check if there is an existing container for the nitro-proxy
			containers, err := docker.ContainerList(ctx, types.ContainerListOptions{Filters: proxyFilter, All: true})
			if err != nil {
				return fmt.Errorf("unable to list the containers\n%w", err)
			}

			// get the container id and determine if the container needs to start
			for _, c := range containers {
				for _, n := range c.Names {
					if n == env || n == "/"+env {
						proxyContainerID := c.ID

						// check if it is running
						if c.State != "running" {
							output.Pending("starting proxy")

							if err := docker.ContainerStart(ctx, proxyContainerID, types.ContainerStartOptions{}); err != nil {
								return fmt.Errorf("unable to start the nitro container, %w", err)
							}

							output.Done()
						} else {
							output.Success("proxy ready")
						}
					}
				}
			}

			// check the databases
			output.Info("Checking Databases...")
			for _, db := range cfg.Databases {
				// add filters to check for the container
				filter.Add("label", labels.DatabaseEngine+"="+db.Engine)
				filter.Add("label", labels.DatabaseVersion+"="+db.Version)
				filter.Add("label", labels.Type+"=database")

				// get the containers for the database
				containers, err := docker.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: filter})
				if err != nil {
					return fmt.Errorf("error getting a list of containers")
				}

				// set the hostname for the database container
				hostname, err := db.GetHostname()
				if err != nil {
					return err
				}

				// if there is not a container for the database, create a volume, container, and start the container
				switch len(containers) {
				// the database container exists
				case 1:
					// check if the container is running
					if containers[0].State != "running" {
						output.Pending("starting", hostname)

						// start the container
						if err := docker.ContainerStart(ctx, containers[0].ID, types.ContainerStartOptions{}); err != nil {
							output.Warning()
							return err
						}

						output.Done()
					} else {
						output.Success(hostname, "ready")
					}
				default:
					// database container does not exist, so create the volume and start it
					output.Pending("creating volume", hostname)

					// create the database labels
					lbls := map[string]string{
						labels.Environment:     env,
						labels.DatabaseEngine:  db.Engine,
						labels.DatabaseVersion: db.Version,
						labels.Type:            "database",
					}

					// if the database is mysql or mariadb, mark them as
					// mysql compatible (used for importing backups)
					if db.Engine == "mariadb" || db.Engine == "mysql" {
						lbls[labels.DatabaseCompatability] = "mysql"
					}

					// if the database is postgres, mark it as compatible
					// with postgres. This is not needed but a place holder
					// if cockroachdb is ever supported by craft.
					if db.Engine == "postgres" {
						lbls[labels.DatabaseCompatability] = "postgres"
					}

					// create the volume
					volume, err := docker.VolumeCreate(ctx, volumetypes.VolumesCreateBody{Driver: "local", Name: hostname, Labels: lbls})
					if err != nil {
						return fmt.Errorf("unable to create the volume, %w", err)
					}

					output.Done()

					// determine the image name
					image := fmt.Sprintf(DatabaseImage, db.Engine, db.Version)

					// set mounts and environment based on the database type
					target := "/var/lib/mysql"
					var envs []string
					if strings.Contains(image, "postgres") {
						target = "/var/lib/postgresql/data"
						envs = []string{"POSTGRES_USER=nitro", "POSTGRES_DB=nitro", "POSTGRES_PASSWORD=nitro"}
					} else {
						envs = []string{"MYSQL_ROOT_PASSWORD=nitro", "MYSQL_DATABASE=nitro", "MYSQL_USER=nitro", "MYSQL_PASSWORD=nitro"}
					}

					// check for if we should skip pulling an image
					if cmd.Flag("skip-pull").Value.String() == "false" {
						output.Pending("pulling", image)

						// pull the image
						rdr, err := docker.ImagePull(ctx, image, types.ImagePullOptions{All: false})
						if err != nil {
							output.Warning()
							return fmt.Errorf("unable to pull image %s, %w", image, err)
						}

						// read the output to pull the image
						buf := &bytes.Buffer{}
						if _, err := buf.ReadFrom(rdr); err != nil {
							output.Warning()
							return fmt.Errorf("unable to read output from pulling image %s, %w", image, err)
						}

						output.Done()
					}

					output.Pending("creating", hostname)

					// set the port for the database
					port, err := nat.NewPort("tcp", db.Port)
					if err != nil {
						output.Warning()
						return fmt.Errorf("unable to create the port, %w", err)
					}
					containerConfig := &container.Config{
						Image:  image,
						Labels: lbls,
						ExposedPorts: nat.PortSet{
							port: struct{}{},
						},
						Env: envs,
					}
					hostConfig := &container.HostConfig{
						Mounts: []mount.Mount{
							{
								Type:   mount.TypeVolume,
								Source: volume.Name,
								Target: target,
							},
						},
						PortBindings: map[nat.Port][]nat.PortBinding{
							port: {
								{
									HostIP:   "127.0.0.1",
									HostPort: db.Port,
								},
							},
						},
					}
					networkConfig := &network.NetworkingConfig{
						EndpointsConfig: map[string]*network.EndpointSettings{
							env: {
								NetworkID: networkID,
							},
						},
					}

					// create the container for the database
					if _, err := create(ctx, docker, containerConfig, hostConfig, networkConfig, hostname); err != nil {
						output.Warning()
						return err
					}

					output.Done()
				}

				// remove the database filters
				filter.Del("label", labels.DatabaseEngine+"="+db.Engine)
				filter.Del("label", labels.DatabaseVersion+"="+db.Version)
				filter.Del("label", labels.Type+"=database")
			}

			output.Info("Checking Services...")

			// check if the mailhog service should be created
			switch cfg.Services.Mailhog {
			case true:
				// add the filter for mailhog
				filter.Add("label", labels.Type+"=mailhog")

				// get a list of containers
				containers, err := docker.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: filter})
				if err != nil {
					return err
				}

				// if there is no container, create it
				switch len(containers) {
				case 0:
					output.Pending("creating mailhog service")

					// pull the mailhog image
					rdr, err := docker.ImagePull(ctx, "docker.io/mailhog/mailhog", types.ImagePullOptions{})
					if err != nil {
						return err
					}

					buf := &bytes.Buffer{}
					if _, err := buf.ReadFrom(rdr); err != nil {
						return fmt.Errorf("unable to read the output from pulling the image, %w", err)
					}

					// configure the service
					smtpPort, err := nat.NewPort("tcp/udp", "1025")
					if err != nil {
						output.Warning()
						return fmt.Errorf("unable to create the port, %w", err)
					}
					httpPort, err := nat.NewPort("tcp", "8025")
					if err != nil {
						output.Warning()
						return fmt.Errorf("unable to create the port, %w", err)
					}
					containerConfig := &container.Config{
						Image: "docker.io/mailhog/mailhog",
						Labels: map[string]string{
							labels.Environment: env,
							labels.Type:        "mailhog",
						},
						ExposedPorts: nat.PortSet{
							smtpPort: struct{}{},
							httpPort: struct{}{},
						},
					}
					hostconfig := &container.HostConfig{
						PortBindings: map[nat.Port][]nat.PortBinding{
							smtpPort: {
								{
									HostIP:   "127.0.0.1",
									HostPort: "1025",
								},
							},
							httpPort: {
								{
									HostIP:   "127.0.0.1",
									HostPort: "8025",
								},
							},
						},
					}
					networkConfig := &network.NetworkingConfig{
						EndpointsConfig: map[string]*network.EndpointSettings{
							env: {
								NetworkID: networkID,
							},
						},
					}

					// create the container
					if _, err := create(ctx, docker, containerConfig, hostconfig, networkConfig, "mailhog"); err != nil {
						output.Warning()
						return err
					}

					output.Done()
				default:
					// check if the container is running
					if containers[0].State != "running" {
						// start the container
						output.Pending("starting mailhog")

						// start the container
						if err := docker.ContainerStart(ctx, containers[0].ID, types.ContainerStartOptions{}); err != nil {
							output.Warning()
							break
						}

						output.Done()
					} else {
						output.Success("mailhog ready")
					}
				}

				// remove the label
				filter.Del("label", labels.Type+"=mailhog")
			default:
				// add the filter for mailhog
				filter.Add("label", labels.Type+"=mailhog")

				// check if there is an existing container for mailhog
				containers, err := docker.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: filter})
				if err != nil {
					return err
				}

				// if we have a container, we need to remove it
				if len(containers) > 0 {
					output.Pending("removing mailhog")

					// stop the container
					if err := docker.ContainerStop(ctx, containers[0].ID, nil); err != nil {
						output.Warning()
						output.Info(err.Error())
					}

					// remove the container
					if err := docker.ContainerRemove(ctx, containers[0].ID, types.ContainerRemoveOptions{RemoveVolumes: true}); err != nil {
						output.Warning()
						output.Info(err.Error())
					}

					// remove the label
					filter.Del("label", labels.Type+"=mailhog")
				}
			}

			// get all of the sites, their local path, the php version, and the type of project (nginx or PHP-FPM)
			output.Info("Checking Sites...")

			// get the envs for the sites
			envs := cfg.AsEnvs()

			for _, site := range cfg.Sites {
				// add the site filter
				filter.Add("label", labels.Host+"="+site.Hostname)

				// look for a container for the site
				containers, err := docker.ContainerList(ctx, types.ContainerListOptions{All: true, Filters: filter})
				if err != nil {
					return fmt.Errorf("error getting a list of containers")
				}

				switch len(containers) {
				case 1:
					// there is a running container
					c := containers[0]
					image := fmt.Sprintf(NginxImage, site.PHP)

					// get the containers details that include environment variables
					details, err := docker.ContainerInspect(ctx, c.ID)
					if err != nil {
						return err
					}

					// make sure the images and mounts match, if they don't stop, remove, and recreate the container
					if match.Site(home, site, cfg.PHP, details) == false {
						output.Pending(site.Hostname, "out of sync")

						path, err := site.GetAbsPath(home)
						if err != nil {
							return err
						}

						// stop container
						if err := docker.ContainerStop(ctx, c.ID, nil); err != nil {
							return err
						}

						// remove container
						if err := docker.ContainerRemove(ctx, c.ID, types.ContainerRemoveOptions{}); err != nil {
							return err
						}

						output.Done()

						// pull the image
						if cmd.Flag("skip-pull").Value.String() == "false" {
							output.Pending("pulling", image)

							// pull the image
							rdr, err := docker.ImagePull(ctx, image, types.ImagePullOptions{All: false})
							if err != nil {
								return fmt.Errorf("unable to pull image, %w", err)
							}

							// read to pull the image
							buf := &bytes.Buffer{}
							if _, err := buf.ReadFrom(rdr); err != nil {
								return fmt.Errorf("unable to read output from pulling image %s, %w", image, err)
							}

							output.Done()
						}

						// add the path mount
						mounts := []mount.Mount{}
						mounts = append(mounts, mount.Mount{
							Type:   mount.TypeBind,
							Source: path,
							Target: "/app",
						})

						// get additional site mounts
						siteMounts, err := site.GetAbsMountPaths(home)
						if err != nil {
							return err
						}

						// create mounts for the site
						for k, v := range siteMounts {
							mounts = append(mounts, mount.Mount{
								Type:   mount.TypeBind,
								Source: k,
								Target: v,
							})
						}

						// check if xdebug is enabled
						if site.Xdebug {
							envs = append(envs, "XDEBUG_MODE=develop,debug")
						} else {
							envs = append(envs, "XDEBUG_MODE=off")
						}

						// add the site itself to the extra hosts
						extraHosts := []string{fmt.Sprintf("%s:%s", site.Hostname, "127.0.0.1")}
						for _, s := range site.Aliases {
							extraHosts = append(extraHosts, fmt.Sprintf("%s:%s", s, "127.0.0.1"))
						}

						// create the container config
						containerConfig := &container.Config{
							Image: image,
							Labels: map[string]string{
								labels.Environment: env,
								labels.Host:        site.Hostname,
							},
							Env: envs,
						}
						hostConfig := &container.HostConfig{
							Mounts:     mounts,
							ExtraHosts: extraHosts,
						}
						networkConfig := &network.NetworkingConfig{
							EndpointsConfig: map[string]*network.EndpointSettings{
								env: {
									NetworkID: networkID,
								},
							},
						}

						output.Pending("creating", site.Hostname)

						// create the container
						if _, err := create(ctx, docker, containerConfig, hostConfig, networkConfig, site.Hostname); err != nil {
							output.Warning()

							return fmt.Errorf("unable to create the site, %w", err)
						}

						output.Done()

						break
					}
				default:
					// create a brand new container since there is not an existing one
					image := fmt.Sprintf(NginxImage, site.PHP)

					// should we skip pulling the image
					if cmd.Flag("skip-pull").Value.String() == "false" {
						output.Pending("pulling", image)

						// pull the image
						rdr, err := docker.ImagePull(ctx, image, types.ImagePullOptions{All: false})
						if err != nil {
							return fmt.Errorf("unable to pull the image, %w", err)
						}

						buf := &bytes.Buffer{}
						if _, err := buf.ReadFrom(rdr); err != nil {
							return fmt.Errorf("unable to read output from pulling image %s, %w", image, err)
						}

						output.Done()
					}

					// get the sites main path
					path, err := site.GetAbsPath(home)
					if err != nil {
						return err
					}

					output.Pending("creating", site.Hostname)

					// add the path mount
					mounts := []mount.Mount{}
					mounts = append(mounts, mount.Mount{
						Type:   mount.TypeBind,
						Source: path,
						Target: "/app",
					})

					// get additional site mounts
					siteMounts, err := site.GetAbsMountPaths(home)
					if err != nil {
						return err
					}

					for k, v := range siteMounts {
						mounts = append(mounts, mount.Mount{
							Type:   mount.TypeBind,
							Source: k,
							Target: v,
						})
					}

					// check if xdebug is enabled
					if site.Xdebug {
						envs = append(envs, "XDEBUG_MODE=develop,debug")
					} else {
						envs = append(envs, "XDEBUG_MODE=off")
					}

					// add the site itself to the extra hosts
					extraHosts := []string{fmt.Sprintf("%s:%s", site.Hostname, "127.0.0.1")}
					for _, s := range site.Aliases {
						extraHosts = append(extraHosts, fmt.Sprintf("%s:%s", s, "127.0.0.1"))
					}

					// create the container config
					containerConfig := &container.Config{
						Image: image,
						Labels: map[string]string{
							labels.Environment: env,
							labels.Host:        site.Hostname,
						},
						Env: envs,
					}
					hostConfig := &container.HostConfig{
						Mounts:     mounts,
						ExtraHosts: extraHosts,
					}
					networkConfig := &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{env: {NetworkID: networkID}}}

					// create the container
					if _, err := create(ctx, docker, containerConfig, hostConfig, networkConfig, site.Hostname); err != nil {
						output.Warning()

						return fmt.Errorf("unable to create the site, %w", err)
					}

					output.Done()
				}

				// remove the site filter
				filter.Del("label", labels.Host+"="+site.Hostname)
			}

			// convert the sites into the gRPC API Apply request
			sites := make(map[string]*protob.Site)
			for _, s := range cfg.Sites {
				hosts := []string{s.Hostname}

				// if there are aliases lets append them to the hosts
				if len(s.Aliases) > 0 {
					hosts = append(hosts, s.Aliases...)
				}

				// create the site
				sites[s.Hostname] = &protob.Site{
					Hostname: s.Hostname,
					Aliases:  strings.Join(hosts, ","),
					Port:     8080,
				}
			}

			output.Pending("waiting for nitrod")

			// ping the nitrod API until its ready...
			for {
				_, err := nitrod.Ping(ctx, &protob.PingRequest{})
				if err == nil {
					break
				}
			}

			output.Done()

			output.Info("Configuring Proxy...")

			// configure the proxy with the sites
			if _, err = nitrod.Apply(ctx, &protob.ApplyRequest{Sites: sites}); err != nil {
				return err
			}

			output.Success("proxy ready")

			// update the hosts files
			if os.Getenv("NITRO_EDIT_HOSTS") == "false" || cmd.Flag("skip-hosts").Value.String() == "true" {
				// skip updating the hosts file
				return nil
			}

			// get all possible hostnames
			var hostnames []string
			for _, s := range cfg.Sites {
				hostnames = append(hostnames, s.Hostname)
				hostnames = append(hostnames, s.Aliases...)
			}

			// get the executable
			nitro, err := os.Executable()
			if err != nil {
				return fmt.Errorf("unable to locate the nitro path, %w", err)
			}

			// run the hosts command
			switch runtime.GOOS {
			case "windows":
				return fmt.Errorf("setting hosts file is not yet supported on windows")
			default:
				output.Info("Modifying hosts file (you might be prompted for your password)")

				// add the hosts
				if err := sudo.Run(nitro, "nitro", "hosts", "--hostnames="+strings.Join(hostnames, ",")); err != nil {
					return err
				}
			}

			output.Info(env, "is up and running 😃")

			return nil
		},
	}

	// add flag to skip pulling images
	cmd.Flags().Bool("skip-pull", false, "skip pulling images")
	cmd.Flags().Bool("skip-hosts", false, "skip modifying the hosts file")

	return cmd
}

func create(ctx context.Context, docker client.ContainerAPIClient, config *container.Config, host *container.HostConfig, network *network.NetworkingConfig, name string) (string, error) {
	// create the container
	resp, err := docker.ContainerCreate(ctx, config, host, network, name)
	if err != nil {
		return "", fmt.Errorf("unable to create the container, %w", err)
	}

	// start the container
	if err := docker.ContainerStart(ctx, resp.ID, types.ContainerStartOptions{}); err != nil {
		return "", fmt.Errorf("unable to start the container, %w", err)
	}

	return resp.ID, nil
}
