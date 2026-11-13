package main

import (
	"errors"
	"io/fs"
	"net/url"
	"path"
	"strings"
)

type adminMarkdownPage struct {
	Path   string `json:"path"`
	URL    string `json:"url"`
	Cached bool   `json:"cached"`
}

func listMarkdownPages() ([]adminMarkdownPage, error) {
	siteMutex.RLock()
	defer siteMutex.RUnlock()

	var pages []adminMarkdownPage

	err := walkMarkdownSources(func(name string) error {
		cached, err := public.Lstat(markdownCachePath(name))
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}

		page := adminMarkdownPage{
			Path:   name,
			URL:    markdownPageURL(name),
			Cached: err == nil && cached.Mode().IsRegular(),
		}
		pages = append(pages, page)

		return nil
	})

	return pages, err
}

func clearMarkdownPageCache(name string) error {
	if !validMarkdownSource(name) {
		return fs.ErrInvalid
	}

	// The same barrier as a full clear prevents an in-flight render from
	// publishing stale HTML after this page's cache has been removed.
	siteMutex.Lock()
	defer siteMutex.Unlock()

	current := ""

	for component := range strings.SplitSeq(name, "/") {
		current = path.Join(current, component)

		info, err := public.Lstat(current)
		if err != nil {
			return err
		}

		if info.Mode()&fs.ModeSymlink != 0 || current == name && !info.Mode().IsRegular() {
			return fs.ErrInvalid
		}
	}

	cache := markdownCachePath(name)
	frontendCache.Delete(cache)
	resetSitemapCache()

	info, err := public.Lstat(cache)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fs.ErrInvalid
	}

	return public.Remove(cache)
}

// Callers hold the site lock through enumeration and use of the source paths.
func walkMarkdownSources(visit func(string) error) error {
	return fs.WalkDir(public.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if reservedAdminSource(name) {
			if entry.IsDir() {
				return fs.SkipDir
			}

			return nil
		}

		if !entry.Type().IsRegular() || !validMarkdownSource(name) {
			return nil
		}

		return visit(name)
	})
}

func validMarkdownSource(name string) bool {
	return fs.ValidPath(name) && !strings.ContainsAny(name, "\\:") && strings.HasSuffix(name, ".md") && !reservedAdminSource(name)
}

func reservedAdminSource(name string) bool {
	first, _, _ := strings.Cut(name, "/")
	first = strings.TrimRight(first, ". ")

	return strings.EqualFold(first, "assets") || first == "-"
}

func markdownCachePath(name string) string {
	return strings.TrimSuffix(name, ".md") + ".html"
}

func markdownPageURL(name string) string {
	pagePath := "/" + strings.TrimSuffix(name, ".md")
	if config.Frontend.RedirectIndex && path.Base(pagePath) == "index" {
		pagePath = strings.TrimSuffix(pagePath, "index")
	}

	target := url.URL{Path: pagePath}

	return target.EscapedPath()
}
