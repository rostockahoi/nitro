package containerlabels

import (
	"strings"

	"github.com/craftcms/nitro/pkg/config"
)

const (
	// Nitro is used to label a container as "for nitro"
	Nitro = "com.craftcms.nitro"

	// NitroContainer is used to identify a custom container added to the config
	NitroContainer = "com.craftcms.nitro.container"

	// NitroContainerPort is used to identify a custom containers port in the config
	NitroContainerPort = "com.craftcms.nitro.container-port"

	// DatabaseCompatibility is the compatibility of the database (e.g. mariadb and mysql are compatible)
	DatabaseCompatibility = "com.craftcms.nitro.database-compatibility"

	// DatabaseEngine is used to identify the engine that is being used for a database container (e.g. mysql, postgres)
	DatabaseEngine = "com.craftcms.nitro.database-engine"

	// DatabasePort is used to identify the port that is being used for a database container (e.g. mysql, postgres)
	DatabasePort = "com.craftcms.nitro.database-port"

	// DatabaseVersion is the version of the database the container is running (e.g. 11, 12, 5.7)
	DatabaseVersion = "com.craftcms.nitro.database-version"

	// Extensions is used for a list of comma seperated extensions for a site
	Extensions = "com.craftcms.nitro.extensions"

	// Host is used to identify a web application by the hostname of the site (e.g demo.nitro)
	Host = "com.craftcms.nitro.host"

	// PAth is used for containers that mount specific paths such as composer and npm
	Path = "com.craftcms.nitro.path"

	// Network is used to label a network for an environment
	Network = "com.craftcms.nitro.network"

	// Volume is used to identify a volume for an environment
	Volume = "com.craftcms.nitro.volume"

	// Proxy is the label used to identify the proxy container
	Proxy = "com.craftcms.nitro.proxy"

	// ProxyVersion is used to label a proxy container with a specific version
	ProxyVersion = "com.craftcms.nitro.proxy-version"

	// Type is used to identity the type of container
	Type = "com.craftcms.nitro.type"
)

// ForSite takes a site and returns labels to use on the sites container.
func ForSite(s config.Site) map[string]string {
	labels := map[string]string{
		Nitro: "true",
		Host:  s.Hostname,
	}

	// if there are extensions, add them as comma separated
	if len(s.Extensions) > 0 {
		labels[Extensions] = strings.Join(s.Extensions, ",")
	}

	return labels
}

// ForCustomContainer takes a custom container configuration and
// applies the labels for the container.
func ForCustomContainer(c config.Container) map[string]string {
	labels := map[string]string{
		Nitro:          "true",
		Type:           "custom",
		NitroContainer: c.Name,
	}

	return labels
}
