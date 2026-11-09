package main

import (
	"io"
	"io/fs"
	"path/filepath"

	"github.com/CloudyKit/jet/v6"
)

func NewLoaderFromFS(root fs.FS) (jet.Loader, error) {
	loader := jet.NewInMemLoader()

	err := LoaderWalkDirFS(root, ".", loader)
	if err != nil {
		return nil, err
	}

	return loader, nil
}

func LoaderWalkDirFS(root fs.FS, dir string, loader *jet.InMemLoader) error {
	return fs.WalkDir(root, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		name := d.Name()

		if name == "assets" {
			return fs.SkipDir
		} else if filepath.Ext(name) != ".html" {
			return nil
		}

		if d.IsDir() {
			return LoaderWalkDirFS(root, path, loader)
		} else {
			file, err := root.Open(path)
			if err != nil {
				return err
			}

			defer file.Close()

			b, err := io.ReadAll(file)
			if err != nil {
				return err
			}

			loader.Set(path, string(b))
		}

		return nil
	})
}
