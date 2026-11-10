package main

import (
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
			if path == "error.html" {
				log.Warnln(err)

				return nil
			}

			return err
		}

		name := d.Name()

		if d.IsDir() {
			if name == "assets" || path == "error.html" {
				return fs.SkipDir
			}

			return nil
		}

		if filepath.Ext(name) != ".html" {
			return nil
		}

		b, err := fs.ReadFile(root, path)
		if err != nil {
			if path == "error.html" {
				log.Warnln(err)

				return nil
			}

			return err
		}

		loader.Set(path, string(b))

		return nil
	})
}
