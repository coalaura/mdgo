package main

import (
	"embed"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/CloudyKit/jet/v6"
)

type Theme struct {
	layout *jet.Template
	assets fs.FS
}

var (
	//go:embed themes/default
	ThemeDefaultFS embed.FS
)

func LoadTheme() (*Theme, error) {
	if config.Server.Theme == "" || config.Server.Theme == "default" {
		root, err := fs.Sub(ThemeDefaultFS, "themes/default")
		if err != nil {
			return nil, err
		}

		return LoadThemeFromFS(root)
	}

	themes, err := os.OpenRoot("themes")
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errors.New("missing themes folder")
		}

		return nil, err
	}

	root, err := themes.OpenRoot(config.Server.Theme)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("theme %q does not exist", config.Server.Theme)
		}

		return nil, err
	}

	return LoadThemeFromFS(root.FS())
}

func LoadThemeFromFS(root fs.FS) (*Theme, error) {
	loader, err := NewLoaderFromFS(root)
	if err != nil {
		return nil, err
	}

	templates := jet.NewSet(loader, jet.WithTemplateNameExtensions([]string{""}))

	layout, err := templates.GetTemplate("layout.html")
	if err != nil {
		return nil, err
	}

	err = layout.Execute(io.Discard, nil, NewThemeData(nil, nil))
	if err != nil {
		return nil, err
	}

	assets, err := LoadThemeAssets(root)
	if err != nil {
		return nil, err
	}

	return &Theme{
		layout: layout,
		assets: assets,
	}, nil
}

func LoadThemeAssets(root fs.FS) (fs.FS, error) {
	info, err := fs.Stat(root, "assets")
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

func NewThemeData(content []byte, page map[string]any) map[string]any {
	return map[string]any{
		"content": content,
		"page":    page,
	}
}
