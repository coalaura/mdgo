package main

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"net/netip"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
)

const ConfigPath = "config.yml"

// gost:preserve-layout
type ConfigServer struct {
	Port   int64 `yaml:"port"`
	Secure bool  `yaml:"secure"`
}

type ConfigTheme struct {
	Name   string       `yaml:"name"`
	Assets ConfigAssets `yaml:"assets"`
}

type ConfigSite struct {
	URL string `yaml:"url"`
}

type ConfigAssets struct {
	Compile     bool     `yaml:"compile"`
	Minify      bool     `yaml:"minify"`
	Entrypoints []string `yaml:"entrypoints"`
}

type configValues Config

type legacyThemeName struct {
	Name *string `yaml:"name"`
}

type legacyServerTheme struct {
	Theme *string `yaml:"theme"`
}

type configMigration struct {
	Server legacyServerTheme `yaml:"server"`
	Theme  legacyThemeName   `yaml:"theme"`
}

// gost:preserve-layout
type ConfigFrontend struct {
	RedirectHTML  bool `yaml:"redirect_html"`
	RedirectIndex bool `yaml:"redirect_index"`
}

// gost:preserve-layout
type ConfigTokens struct {
	Auth string `yaml:"auth"`
}

// gost:preserve-layout
type ConfigTLDs []string

// gost:preserve-layout
type Config struct {
	Server   ConfigServer   `yaml:"server"`
	Site     ConfigSite     `yaml:"site"`
	Theme    ConfigTheme    `yaml:"theme"`
	Frontend ConfigFrontend `yaml:"frontend"`
	Tokens   ConfigTokens   `yaml:"tokens"`
}

func (c *Config) UnmarshalYAML(data []byte) error {
	values := configValues(*c)

	err := yaml.Unmarshal(data, &values)
	if err != nil {
		return err
	}

	var migration configMigration

	err = yaml.Unmarshal(data, &migration)
	if err != nil {
		return err
	}

	legacy := migration.Server.Theme
	selected := migration.Theme.Name

	if legacy != nil {
		name := normalizeThemeName(*legacy)
		if selected != nil && name != normalizeThemeName(*selected) {
			return fmt.Errorf("conflicting server.theme %q and theme.name %q", *legacy, *selected)
		}

		if selected == nil {
			values.Theme.Name = name
		}
	}

	values.Theme.Name = normalizeThemeName(values.Theme.Name)

	*c = Config(values)

	err = c.Theme.Validate()
	if err != nil {
		return err
	}

	return c.Site.Validate()
}

func (c ConfigSite) Validate() error {
	if c.URL == "" {
		return nil
	}

	base, err := url.Parse(c.URL)
	if err != nil {
		return fmt.Errorf("invalid site.url: %w", err)
	}

	validScheme := base.Scheme == "http" || base.Scheme == "https"
	if !validScheme || base.Hostname() == "" || base.Opaque != "" || base.User != nil || strings.ContainsAny(c.URL, "?#\\") || !utf8.ValidString(c.URL) || !utf8.ValidString(base.Path) {
		return fmt.Errorf("invalid site.url: use an absolute HTTP(S) URL without credentials, query or fragment")
	}

	if strings.ContainsAny(base.Host, "[]") || strings.Contains(base.Hostname(), ":") {
		_, err = netip.ParseAddr(base.Hostname())
		if err != nil || !strings.HasPrefix(base.Host, "[") {
			return fmt.Errorf("invalid site.url IP address")
		}
	}

	port := base.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return fmt.Errorf("invalid site.url port %q", port)
		}
	} else if strings.HasSuffix(base.Host, ":") {
		return fmt.Errorf("invalid site.url: empty port")
	}

	for component := range strings.SplitSeq(base.Path, "/") {
		if component == "." || component == ".." || strings.Contains(component, "\\") {
			return fmt.Errorf("invalid site.url path prefix")
		}
	}

	for _, character := range base.Path {
		if character < 0x20 || character == 0x7f {
			return fmt.Errorf("invalid site.url path prefix")
		}
	}

	return nil
}

func (c ConfigTheme) Validate() error {
	if !validAssetPath(c.Name) {
		return fmt.Errorf("invalid theme.name %q", c.Name)
	}

	return c.Assets.Validate()
}

func (c ConfigAssets) Validate() error {
	if c.Compile && len(c.Entrypoints) == 0 {
		return fmt.Errorf("theme.assets.entrypoints are required when compile is true")
	}

	if !c.Compile && len(c.Entrypoints) != 0 {
		return fmt.Errorf("theme.assets.entrypoints must be empty when compile is false")
	}

	seen := make(map[string]bool, len(c.Entrypoints))

	for _, name := range c.Entrypoints {
		extension := path.Ext(name)
		if !validAssetPath(name) || extension != ".js" && extension != ".mjs" {
			return fmt.Errorf("invalid theme entrypoint %q: use an assets-relative .js or .mjs path", name)
		}

		if seen[name] {
			return fmt.Errorf("duplicate theme entrypoint %q", name)
		}

		seen[name] = true
	}

	return nil
}

func (c *Config) Addr() string {
	return fmt.Sprintf(":%d", c.Server.Port)
}

func (c *Config) Init() error {
	var store bool

	err := c.Theme.Validate()
	if err != nil {
		return err
	}

	err = c.Site.Validate()
	if err != nil {
		return err
	}

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
			"$.site":     {yaml.HeadComment("")},
			"$.theme":    {yaml.HeadComment("")},
			"$.frontend": {yaml.HeadComment("", " managed fingerprinted assets: public, max-age=31536000, immutable; other public responses: no-cache", " admin, authenticated/private responses and errors: no-store", " page and sitemap ETags support revalidation; rendered HTML has no Last-Modified", " clearing the server cache invalidates pages, validators and sitemap data and removes obsolete assets; it does not purge browser caches")},
			"$.tokens":   {yaml.HeadComment("")},

			"$.server.port":   {yaml.HeadComment(" port to serve mdgo on (required; default 2727)")},
			"$.server.secure": {yaml.HeadComment(" if mdgo is being served with TLS (default: false)")},

			"$.site.url": {yaml.HeadComment(" public absolute HTTP(S) URL without credentials, query or fragment; empty disables /sitemap.xml", " example: https://example.com/docs; the path prefix is retained in canonical sitemap URLs", " sitemap: sorted, deduplicated routable Markdown pages (including the homepage), excluding admin/assets and redirects", " UTF-8 XML contains loc only; generated on first request and reused until a page/full cache clear or restart", " limits: 50,000 URLs, 50 MiB uncompressed, each URL shorter than 2048 characters; overflow returns HTTP 500 and logs the cause")},

			"$.theme.name":               {yaml.HeadComment(" mdgo theme (default: \"default\")")},
			"$.theme.assets.compile":     {yaml.HeadComment(" bundle declared JavaScript entrypoints and dependencies")},
			"$.theme.assets.minify":      {yaml.HeadComment(" minify JavaScript and CSS (default true)")},
			"$.theme.assets.entrypoints": {yaml.HeadComment(" assets-relative .js/.mjs paths; required only when compile is true")},

			"$.frontend.redirect_html":  {yaml.HeadComment(" redirect .html URLs to extensionless paths (default true)")},
			"$.frontend.redirect_index": {yaml.HeadComment(" redirect index, index.html and index.md URLs to their directory; overrides redirect_html (default true)")},

			"$.tokens.auth": {yaml.HeadComment(" auth token for login; auto-generated if empty")},
		}
	)

	err := yaml.NewEncoder(&buffer, yaml.WithComment(comments)).Encode(c)
	if err != nil {
		return err
	}

	body := bytes.ReplaceAll(buffer.Bytes(), []byte("#\n"), []byte("\n"))

	return os.WriteFile(ConfigPath, body, 0600)
}

func DefaultConfig() *Config {
	return &Config{
		Server: ConfigServer{
			Port:   2727,
			Secure: false,
		},
		Theme: ConfigTheme{
			Name: "default",
			Assets: ConfigAssets{
				Minify:      true,
				Entrypoints: []string{},
			},
		},
		Frontend: ConfigFrontend{
			RedirectHTML:  true,
			RedirectIndex: true,
		},
	}
}

func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	file, err := os.OpenFile(ConfigPath, os.O_RDONLY, 0)
	if err == nil {
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return nil, err
		}

		if info.Mode() != 0600 {
			err = os.Chmod(ConfigPath, 0600)
			if err != nil {
				return nil, err
			}
		}

		err = yaml.NewDecoder(file).Decode(cfg)
		if err != nil {
			return nil, err
		}
	} else {
		if !os.IsNotExist(err) {
			return nil, err
		}

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

func normalizeThemeName(name string) string {
	if name == "" {
		return "default"
	}

	return name
}

func validAssetPath(name string) bool {
	if name == "." || !fs.ValidPath(name) || strings.ContainsAny(name, "\\:*?\"<>|\x00#%") {
		return false
	}

	for component := range strings.SplitSeq(name, "/") {
		if strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") || strings.HasPrefix(component, ".mdgo") || component == ".manifest" {
			return false
		}
	}

	return true
}
