package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"io"
	"io/fs"
	"os"
	"path"
)

type AssetSyncer struct {
	root     *os.Root
	source   fs.FS
	manifest *Manifest
	digest   hash.Hash64
	buffer   []byte
}

func (s *AssetSyncer) syncFile(name string, info fs.FileInfo) (ManifestEntry, error) {
	entry := ManifestEntry{
		Name: name,
		Size: info.Size(),
	}

	modTime := info.ModTime()
	if !modTime.IsZero() {
		entry.ModTimeNS = modTime.UnixNano()
	}

	_, err := s.root.Lstat(name)
	exists := err == nil

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return entry, err
	}

	previous, tracked := s.manifest.Get(name)
	if exists && tracked && previous.MatchesMetadata(info) {
		return previous, nil
	}

	if exists && tracked && previous.Size == entry.Size {
		entry.Hash, err = s.hashFile(name)
		if err != nil {
			return entry, err
		}

		if entry.Hash == previous.Hash {
			return entry, nil
		}
	}

	err = s.root.MkdirAll(path.Dir(name), 0755)
	if err != nil {
		return entry, err
	}

	entry.Hash, err = s.copyFile(name)
	return entry, err
}

func (s *AssetSyncer) hashFile(name string) (uint64, error) {
	file, err := s.source.Open(name)
	if err != nil {
		return 0, err
	}

	defer file.Close()

	s.digest.Reset()

	_, err = io.CopyBuffer(s.digest, file, s.buffer)
	return s.digest.Sum64(), err
}

func (s *AssetSyncer) copyFile(name string) (uint64, error) {
	source, err := s.source.Open(name)
	if err != nil {
		return 0, err
	}

	defer source.Close()

	temporaryName := path.Join(path.Dir(name), ".mdgo-"+rand.Text()+".tmp")

	destination, err := s.root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return 0, err
	}

	defer s.root.Remove(temporaryName)
	defer destination.Close()

	s.digest.Reset()

	writer := io.MultiWriter(destination, s.digest)

	_, err = io.CopyBuffer(writer, source, s.buffer)
	if err != nil {
		return 0, err
	}

	err = destination.Close()
	if err != nil {
		return 0, err
	}

	err = s.root.Rename(temporaryName, name)
	return s.digest.Sum64(), err
}

func SyncAssets() error {
	infos, err := readAssetInfos(theme.assets)
	if err != nil {
		return err
	}

	info, err := public.Lstat("assets")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if err == nil && !info.IsDir() {
		err = public.RemoveAll("assets")
		if err != nil {
			return err
		}
	}

	err = public.MkdirAll("assets", 0755)
	if err != nil {
		return err
	}

	root, err := public.OpenRoot("assets")
	if err != nil {
		return err
	}

	defer root.Close()

	manifest, err := readAssetManifest(root)
	if err != nil {
		return err
	}

	err = root.RemoveAll(".manifest")
	if err != nil {
		return err
	}

	err = pruneAssets(root, infos)
	if err != nil {
		return err
	}

	syncer := AssetSyncer{
		root:     root,
		source:   theme.assets,
		manifest: manifest,
		digest:   fnv.New64a(),
		buffer:   make([]byte, 32*1024),
	}

	var writer ManifestWriter

	for name, info := range infos {
		if info.IsDir() {
			err = root.MkdirAll(name, 0755)
			if err != nil {
				return fmt.Errorf("create asset directory %q: %w", name, err)
			}

			continue
		}

		entry, err := syncer.syncFile(name, info)
		if err != nil {
			return fmt.Errorf("sync asset %q: %w", name, err)
		}

		err = writer.Add(entry)
		if err != nil {
			return err
		}
	}

	return writeAssetManifest(root, &writer)
}

func readAssetInfos(source fs.FS) (map[string]fs.FileInfo, error) {
	infos := make(map[string]fs.FileInfo)

	if source == nil {
		return infos, nil
	}

	err := fs.WalkDir(source, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if name == "." {
			return nil
		}

		if !validManifestName(name) || len(name) > 1<<16-1 {
			return fmt.Errorf("invalid asset name %q", name)
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("asset %q must be a regular file or directory", name)
		}

		infos[name] = info

		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("read theme assets: %w", err)
	}

	return infos, nil
}

func readAssetManifest(root *os.Root) (*Manifest, error) {
	info, err := root.Lstat(".manifest")
	if errors.Is(err, fs.ErrNotExist) {
		return &Manifest{}, nil
	}

	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() {
		return &Manifest{}, nil
	}

	file, err := root.Open(".manifest")
	if err != nil {
		return nil, err
	}

	defer file.Close()

	return ReadManifest(file)
}

func pruneAssets(root *os.Root, infos map[string]fs.FileInfo) error {
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if name == "." {
			return nil
		}

		info, exists := infos[name]
		if exists && entry.IsDir() == info.IsDir() && (entry.IsDir() || entry.Type().IsRegular()) {
			return nil
		}

		err := root.RemoveAll(name)
		if err != nil {
			return fmt.Errorf("remove stale asset %q: %w", name, err)
		}

		if entry.IsDir() {
			return fs.SkipDir
		}

		return nil
	})
}

func writeAssetManifest(root *os.Root, writer *ManifestWriter) error {
	temporaryName := ".mdgo-" + rand.Text() + ".tmp"

	file, err := root.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}

	defer root.Remove(temporaryName)
	defer file.Close()

	_, err = writer.WriteTo(file)
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return root.Rename(temporaryName, ".manifest")
}
