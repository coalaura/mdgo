package main

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/CloudyKit/jet/v6"
)

type Theme struct {
	layout      *jet.Template
	errorLayout *jet.Template
	catalogue   *AssetCatalogue
}

var (
	//go:embed themes/default
	ThemeDefaultFS embed.FS
)

func (t *Theme) Render(wr io.Writer, markdown []byte, meta Metadata) error {
	return t.layout.Execute(wr, nil, NewThemeData(markdown, meta))
}

func LoadTheme() (*Theme, error) {
	if config.Theme.Name == "default" {
		root, err := fs.Sub(ThemeDefaultFS, "themes/default")
		if err != nil {
			return nil, err
		}

		return LoadThemeFromFS(root, config.Theme.Assets)
	}

	themes, err := os.OpenRoot("themes")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("missing themes folder")
		}

		return nil, err
	}

	defer themes.Close()

	root, err := themes.OpenRoot(config.Theme.Name)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("theme %q does not exist", config.Theme.Name)
		}

		return nil, err
	}

	defer root.Close()

	return LoadThemeFromFS(root.FS(), config.Theme.Assets)
}

func LoadThemeFromFS(root fs.FS, options ConfigAssets) (*Theme, error) {
	loader, err := NewLoaderFromFS(root)
	if err != nil {
		return nil, err
	}

	templates := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{""}))

	layout, err := templates.GetTemplate("layout.html")
	if err != nil {
		return nil, err
	}

	var rendered bytes.Buffer

	err = layout.Execute(&rendered, nil, NewThemeData(nil, nil))
	if err != nil {
		return nil, err
	}

	assets, err := LoadThemeAssets(root)
	if err != nil {
		return nil, err
	}

	catalogue, err := BuildThemeAssets(assets, options)
	if err != nil {
		return nil, err
	}

	_, err = catalogue.RewriteHTML(rendered.Bytes(), "/")
	if err != nil {
		return nil, err
	}

	_, err = defaultErrorTemplate()
	if err != nil {
		log.Warnln(fmt.Errorf("load default error.html: %w", err))
	}

	errorLayout, err := templates.GetTemplate("error.html")
	if err != nil {
		if loader.Exists("error.html") {
			log.Warnln(fmt.Errorf("load theme error.html: %w", err))
		}

		errorLayout = nil
	}

	return &Theme{
		layout:      layout,
		errorLayout: errorLayout,
		catalogue:   catalogue,
	}, nil
}

func LoadThemeAssets(root fs.FS) (fs.FS, error) {
	info, err := fs.Lstat(root, "assets")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("stat theme assets: %w", err)
	}

	if !info.IsDir() {
		return nil, errors.New("theme assets must be a directory")
	}

	return fs.Sub(root, "assets")
}

func NewThemeData(content []byte, meta Metadata) map[string]any {
	return map[string]any{
		"content": content,
		"page":    meta,
	}
}
