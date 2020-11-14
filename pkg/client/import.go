package client

import (
	"fmt"
	"io"

	"github.com/docker/docker/api/types"
	"golang.org/x/net/context"
)

// Import is used to copy a local file into a container at a given path. It automatically enables
// overwriting directories with files. This is used for the `db import` commands.
func (cli *Client) Import(ctx context.Context, containerID string, path string, rdr io.Reader) error {
	cli.out.Info("Copying file to container")

	if err := cli.docker.CopyToContainer(ctx, containerID, path, rdr, types.CopyToContainerOptions{AllowOverwriteDirWithFile: true}); err != nil {
		return fmt.Errorf("unable to copy to the container, %w", err)
	}

	cli.out.Info("  ==> copy completed")

	return nil
}
