package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/goccy/go-yaml"
)

const ConfigPath = "config.yml"

// gost:preserve-layout
type ConfigServer struct {
	Port  int64  `yaml:"port"`
	Theme string `yaml:"theme"`
}

// gost:preserve-layout
type ConfigTokens struct {
	Auth string `yaml:"auth"`
}

// gost:preserve-layout
type ConfigTLDs []string

// gost:preserve-layout
type Config struct {
	mp map[string]struct{}

	Server ConfigServer `yaml:"server"`
	Tokens ConfigTokens `yaml:"tokens"`
}

func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Server.Port)
}

func (c *Config) Init() error {
	var store bool

	// check if port is valid
	if c.Server.Port <= 0 || c.Server.Port >= 65535 {
		return fmt.Errorf("invalid server.port %d", c.Server.Port)
	}

	// check if server secret is set
	if c.Tokens.Auth == "" {
		log.Warnln("Missing tokens.auth, generating new")

		secret, err := CreateConfigAuthToken(32)
		if err != nil {
			return err
		}

		c.Tokens.Auth = secret

		store = true
	}

	if store {
		err := c.Store()
		if err != nil {
			return err
		}
	}

	return nil
}

func (c *Config) Store() error {
	var (
		buffer   bytes.Buffer
		comments = yaml.CommentMap{
			"$.tokens": {yaml.HeadComment("")},

			"$.server.port": {yaml.HeadComment(" port to serve tldler on (required; default 2727)")},

			"$.tokens.auth": {yaml.HeadComment(" auth token for login; auto-generated if empty")},
		}
	)

	err := yaml.NewEncoder(&buffer, yaml.WithComment(comments)).Encode(c)
	if err != nil {
		return err
	}

	body := bytes.ReplaceAll(buffer.Bytes(), []byte("#\n"), []byte("\n"))

	return os.WriteFile(ConfigPath, body, 0644)
}

func DefaultConfig() *Config {
	return &Config{
		Server: ConfigServer{
			Port: 2727,
		},
	}
}

func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	file, err := os.OpenFile(ConfigPath, os.O_RDONLY, 0)
	if err == nil {
		defer file.Close()

		err = yaml.NewDecoder(file).Decode(cfg)
		if err != nil {
			return nil, err
		}
	} else {
		err = cfg.Store()
		if err != nil {
			return nil, err
		}
	}

	err = cfg.Init()
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func CreateSecret(length int) (string, error) {
	key := make([]byte, length)

	_, err := io.ReadFull(rand.Reader, key)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(key), nil
}
