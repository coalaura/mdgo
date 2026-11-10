package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"sync"
)

type assetBackup struct {
	name     string
	contents []byte
	existed  bool
}

var (
	siteMutex    sync.RWMutex
	rebuildMutex sync.Mutex
)

func InitializeTheme() error {
	rebuildMutex.Lock()
	defer rebuildMutex.Unlock()

	next, err := LoadTheme()
	if err != nil {
		return err
	}

	siteMutex.Lock()
	defer siteMutex.Unlock()

	backups, err := publishAssets(public, next.catalogue)
	if err != nil {
		return errors.Join(err, restoreAssets(public, backups))
	}

	theme = next

	return nil
}

func ClearThemeCache() error {
	rebuildMutex.Lock()
	defer rebuildMutex.Unlock()

	next, err := LoadTheme()
	if err != nil {
		return err
	}

	return activateTheme(next)
}

func activateTheme(next *Theme) error {
	siteMutex.Lock()
	defer siteMutex.Unlock()

	backups, err := publishAssets(public, next.catalogue)
	if err != nil {
		return errors.Join(err, restoreAssets(public, backups))
	}

	err = clearHTMLCache(public)
	if err != nil {
		return errors.Join(err, restoreAssets(public, backups))
	}

	theme = next

	return pruneThemeAssets(public, next.catalogue)
}

func publishAssets(root *os.Root, catalogue *AssetCatalogue) ([]assetBackup, error) {
	backups := make([]assetBackup, 0, len(catalogue.originals))

	for name, contents := range catalogue.outputs {
		err := writeAsset(root, name, contents)
		if err != nil {
			return backups, err
		}
	}

	for name, contents := range catalogue.originals {
		previous, err := root.ReadFile(name)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return backups, err
		}

		if err == nil && bytes.Equal(previous, contents) {
			continue
		}

		backups = append(backups, assetBackup{name: name, contents: previous, existed: err == nil})

		err = writeAsset(root, name, contents)
		if err != nil {
			return backups, err
		}
	}

	return backups, nil
}

func restoreAssets(root *os.Root, backups []assetBackup) error {
	var failures []error

	for _, backup := range backups {
		var err error

		if backup.existed {
			err = writeAsset(root, backup.name, backup.contents)
		} else {
			err = root.Remove(backup.name)
			if errors.Is(err, fs.ErrNotExist) {
				err = nil
			}
		}

		if err != nil {
			failures = append(failures, err)
		}
	}

	return errors.Join(failures...)
}

func writeAsset(root *os.Root, name string, contents []byte) error {
	previous, err := root.ReadFile(name)
	if err == nil && bytes.Equal(previous, contents) {
		return nil
	}

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	err = root.MkdirAll(path.Dir(name), 0755)
	if err != nil {
		return err
	}

	temporary := path.Join(path.Dir(name), ".mdgo-"+rand.Text()+".tmp")

	err = root.WriteFile(temporary, contents, 0644)
	if err != nil {
		return err
	}

	defer root.Remove(temporary)

	return root.Rename(temporary, name)
}

func clearHTMLCache(root *os.Root) error {
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if name == "assets" {
			return fs.SkipDir
		}

		if !entry.Type().IsRegular() || !strings.HasSuffix(name, ".html") {
			return nil
		}

		info, err := root.Stat(strings.TrimSuffix(name, ".html") + ".md")
		if errors.Is(err, fs.ErrNotExist) {
			generated, markerErr := isGeneratedHTML(root, name)
			if markerErr != nil {
				return markerErr
			}

			if generated {
				return root.Remove(name)
			}

			return nil
		}

		if err != nil {
			return err
		}

		if info.Mode().IsRegular() {
			return root.Remove(name)
		}

		return nil
	})
}

func isGeneratedHTML(root *os.Root, name string) (bool, error) {
	file, err := root.Open(name)
	if err != nil {
		return false, err
	}

	defer file.Close()

	var prefix [len(generatedHTMLMarker)]byte

	_, err = io.ReadFull(file, prefix[:])
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return false, nil
	}

	return string(prefix[:]) == generatedHTMLMarker, err
}

func pruneThemeAssets(root *os.Root, active *AssetCatalogue) error {
	err := fs.WalkDir(root.FS(), assetOutputDirectory, func(name string, entry fs.DirEntry, walkErr error) error {
		if errors.Is(walkErr, fs.ErrNotExist) && name == assetOutputDirectory {
			return nil
		}

		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || activeAsset(active, name) {
			return nil
		}

		err := root.Remove(name)
		if err != nil {
			return fmt.Errorf("prune managed theme asset %q: %w", name, err)
		}

		return nil
	})

	if err != nil {
		return err
	}

	return pruneAssetDirectories(root, assetOutputDirectory)
}

func activeAsset(catalogue *AssetCatalogue, name string) bool {
	_, output := catalogue.outputs[name]
	_, original := catalogue.originals[name]

	return output || original
}

func pruneAssetDirectories(root *os.Root, directory string) error {
	entries, err := fs.ReadDir(root.FS(), directory)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			err = pruneAssetDirectories(root, path.Join(directory, entry.Name()))
			if err != nil {
				return err
			}
		}
	}

	entries, err = fs.ReadDir(root.FS(), directory)
	if err != nil {
		return err
	}

	if directory != assetOutputDirectory && len(entries) == 0 {
		return root.Remove(directory)
	}

	return nil
}
