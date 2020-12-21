package config

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

var (
	// ErrNoConfigFile is returned when a configuration file cannot be found
	ErrNoConfigFile = fmt.Errorf("there is no config file for the environment")

	// ErrNoEnvironmentSet is returned whan an environment is not provided
	ErrNoEnvironmentSet = fmt.Errorf("missing the environment name")
)

// Config represents the nitro-dev.yaml users add for local development.
type Config struct {
	Blackfire Blackfire  `yaml:"blackfire,omitempty"`
	Databases []Database `yaml:"databases,omitempty"`
	Services  Services   `yaml:"services,omitempty"`
	Sites     []Site     `yaml:"sites,omitempty"`

	File string `yaml:"-"`
}

// Blackfire allows users to setup their containers to use blackfire locally.
type Blackfire struct {
	ServerID    string `yaml:"server_id,omitempty"`
	ServerToken string `yaml:"server_token,omitempty"`
}

// PHP is nested in a configuration and allows setting environment variables
// for sites to override in the local development environment.
type PHP struct {
	DisplayErrors         bool   `yaml:"display_errors,omitempty"`
	MaxExecutionTime      int    `yaml:"max_execution_time,omitempty"`
	MaxInputVars          int    `yaml:"max_input_vars,omitempty"`
	MaxInputTime          int    `yaml:"max_input_time,omitempty"`
	MaxFileUpload         string `yaml:"max_file_upload,omitempty"`
	MemoryLimit           string `yaml:"memory_limit,omitempty"`
	OpcacheEnable         bool   `yaml:"opcache_enable,omitempty"`
	OpcacheRevalidateFreq int    `yaml:"opcache_revalidate_freq,omitempty"`
	PostMaxSize           string `yaml:"post_max_size,omitempty"`
	UploadMaxFileSize     string `yaml:"upload_max_file_size,omitempty"`
}

// Services define common tools for development that should run as containers. We don't expose the volumes, ports, and
// networking options for these types of services. We plan to support "custom" container options to make local users
// development even better.
type Services struct {
	Blackfire bool `yaml:"blackfire"`
	DynamoDB  bool `yaml:"dynamodb"`
	Mailhog   bool `yaml:"mailhog"`
	Minio     bool `yaml:"minio"`
	Redis     bool `yaml:"redis"`
}

// Load is used to return the environment name, unmarshalled config, and
// returns an error when trying to get the users home directory or
// while marshalling the config.
func Load(home, env string) (*Config, error) {
	if env == "" {
		return nil, ErrNoEnvironmentSet
	}

	// set the config file
	file := filepath.Join(home, ".nitro", env+".yaml")
	if _, err := os.Stat(file); os.IsNotExist(err) {
		return nil, ErrNoConfigFile
	}

	// create the config
	c := &Config{
		File: file,
	}

	// read the file
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return nil, err
	}

	// unmarshal
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}

	// return the config
	return c, nil
}

// AddSite takes a site and adds it to the config
func (c *Config) AddSite(s Site) error {
	// if there are no aliases
	if len(s.Aliases) == 0 {
		s.Aliases = nil
	}

	// check existing sites
	for _, e := range c.Sites {
		// does the hostname match
		if e.Hostname == s.Hostname {
			return fmt.Errorf("hostname already exists")
		}

		// does the path match
		if e.Path == s.Path {
			return fmt.Errorf("site path already exists")
		}
	}

	// add the site to the list
	c.Sites = append(c.Sites, s)

	return nil
}

// DisableXdebug takes a sites hostname and sets the xdebug option
// to false. If the site cannot be found, it returns an error.
func (c *Config) DisableXdebug(site string) error {
	// find the site by the hostname
	for i, s := range c.Sites {
		if s.Hostname == site {
			// replace the site
			s.Xdebug = false
			c.Sites = append(c.Sites[:i], s)
			return nil
		}
	}

	return fmt.Errorf("unknown site, %s", site)
}

// EnableXdebug takes a sites hostname and sets the xdebug option
// to true. If the site cannot be found, it returns an error.
func (c *Config) EnableXdebug(site string) error {
	// find the site by the hostname
	for i, s := range c.Sites {
		if s.Hostname == site {
			// replace the site
			s.Xdebug = true
			c.Sites = append(c.Sites[:i], s)
			return nil
		}
	}

	return fmt.Errorf("unknown site, %s", site)
}

// Save takes a file path and marshals the config into a file.
func (c *Config) Save() error {
	// make sure the file exists
	if _, err := os.Stat(c.File); os.IsNotExist(err) {
		// otherwise create it
		f, err := os.Create(c.File)
		if err != nil {
			return err
		}
		defer f.Close()

		f.Chown(os.Geteuid(), os.Getuid())
	}

	// unmarshal
	data, err := yaml.Marshal(&c)
	if err != nil {
		return err
	}

	// open the file
	f, err := os.OpenFile(c.File, os.O_SYNC|os.O_WRONLY, os.ModeAppend)
	if err != nil {
		return err
	}

	// write the content
	if _, err := f.Write(data); err != nil {
		return err
	}

	return nil
}

// GetFile returns the file location for the config
func (c *Config) GetFile() string {
	return c.File
}
