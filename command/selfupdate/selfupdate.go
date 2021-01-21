package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"runtime"

	"github.com/minio/selfupdate"
	"github.com/spf13/cobra"

	"github.com/craftcms/nitro/command/version"
	"github.com/craftcms/nitro/pkg/releases"
	"github.com/craftcms/nitro/pkg/terminal"
)

var (
	// LatestURL is the URL to the github releases
	LatestURL = "https://api.github.com/repos/craftcms/nitro/releases/latest"
)

const exampleText = `  # update to the latest version
  nitro self-update`

func NewCommand(output terminal.Outputer) *cobra.Command {
	cmd := &cobra.Command{
		Use:     "self-update",
		Short:   "Update nitro to the latest version",
		Example: exampleText,
		RunE: func(cmd *cobra.Command, args []string) error {
			output.Info("Checking for updates")

			// find the latest release
			release, err := releases.NewFinder().Find(LatestURL, runtime.GOOS, runtime.GOARCH)
			if err != nil {
				return err
			}

			// make sure they are
			if release.Version == version.Version {
				output.Info("up to date!")
				return nil
			}

			output.Pending("found release", release.Version, "updating")

			// create a temp file to save the release into
			file, err := ioutil.TempFile(os.TempDir(), "nitro-release-download-")
			if err != nil {
				log.Fatal(err)
			}
			defer file.Close()

			// download the release
			if err := releases.NewDownloader().Download(release.URL, file.Name()); err != nil {
				log.Fatal(err)
			}

			switch release.ContentType {
			case "application/gzip":
				file, err := os.Open(file.Name())
				if err != nil {
					log.Fatal(err)
				}
				defer file.Close()

				gz, err := gzip.NewReader(file)
				if err != nil {
					log.Fatal(err)
				}
				defer gz.Close()

				// untar the zip
				tr := tar.NewReader(gz)

				i := 0
				for {
					header, err := tr.Next()

					if err == io.EOF {
						break
					}

					if err != nil {
						fmt.Println(err)
						os.Exit(1)
					}

					switch header.Typeflag {
					case tar.TypeDir:
						continue
					case tar.TypeReg:
						name := header.Name

						switch release.OperatingSystem {
						case "windows":
							if name == "nitro.exe" {
								output.Done()

								output.Info("Updating to Nitro", release.Version, "!")

								// self update
								if err := selfupdate.Apply(tr, selfupdate.Options{}); err != nil {
									log.Fatal(err)
								}

								break
							}
						default:
							if name == "nitro" {
								output.Done()

								output.Info("Updating to Nitro", release.Version, "!")

								// self update
								if err := selfupdate.Apply(tr, selfupdate.Options{}); err != nil {
									log.Fatal(err)
								}

								break
							}
						}
					}

					i++
				}
			case "application/zip":
				// unzip
				zr, err := zip.OpenReader(file.Name())
				if err != nil {
					log.Fatal(err)
				}

				for _, file := range zr.Reader.File {
					fmt.Println(file)
				}
			}

			return nil
		},
	}

	return cmd
}
