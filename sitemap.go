package main

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	sitemapURLLimit      = 50000
	sitemapByteLimit     = 50 * 1024 * 1024
	sitemapLocationLimit = 2048
	sitemapNamespace     = "http://www.sitemaps.org/schemas/sitemap/0.9"
)

type sitemapData struct {
	contents []byte
	etag     string
}

type sitemapLocation struct {
	Loc string `xml:"loc"`
}

type sitemapBuffer struct {
	contents bytes.Buffer
}

var cachedSitemap = sync.OnceValues(buildSitemap)

func (buffer *sitemapBuffer) Write(contents []byte) (int, error) {
	if len(contents) > sitemapByteLimit-buffer.contents.Len() {
		return 0, fmt.Errorf("sitemap exceeds the 50 MiB uncompressed limit")
	}

	return buffer.contents.Write(contents)
}

func HandleSitemap(w http.ResponseWriter, r *http.Request) {
	data, err := prepareSitemap()
	if err != nil {
		writeFrontendError(w, r, err)

		return
	}

	w.Header().Set("Content-Type", "application/xml; charset=utf-8")

	setPublicCacheHeaders(w, cacheRevalidate, data.etag)

	http.ServeContent(w, r, "sitemap.xml", time.Time{}, bytes.NewReader(data.contents))
}

func prepareSitemap() (*sitemapData, error) {
	siteMutex.RLock()
	defer siteMutex.RUnlock()

	if config.Site.URL == "" {
		return nil, fs.ErrNotExist
	}

	return cachedSitemap()
}

// Reset under the exclusive site lock; generation holds its read side until
// publication, so an old operation cannot repopulate a cleared cache.
func resetSitemapCache() {
	cachedSitemap = sync.OnceValues(buildSitemap)
}

func buildSitemap() (*sitemapData, error) {
	base, err := url.Parse(config.Site.URL)
	if err != nil {
		return nil, err
	}

	prefix := strings.TrimRight(base.String(), "/")
	locations := make(map[string]struct{})

	err = walkMarkdownSources(func(name string) error {
		location := markdownPageURL(name)

		routable, err := sitemapPageRoutable(name, location)
		if err != nil || !routable {
			return err
		}

		location = prefix + location
		if len(location) >= sitemapLocationLimit {
			return fmt.Errorf("sitemap URL for %q must be shorter than 2048 characters", name)
		}

		locations[location] = struct{}{}

		if len(locations) > sitemapURLLimit {
			return fmt.Errorf("sitemap exceeds the 50,000 URL limit")
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	ordered := make([]string, 0, len(locations))

	for location := range locations {
		ordered = append(ordered, location)
	}

	slices.Sort(ordered)

	var buffer sitemapBuffer

	buffer.contents.WriteString(xml.Header)

	encoder := xml.NewEncoder(&buffer)

	root := xml.StartElement{Name: xml.Name{Local: "urlset"}, Attr: []xml.Attr{{Name: xml.Name{Local: "xmlns"}, Value: sitemapNamespace}}}

	err = encoder.EncodeToken(root)
	if err != nil {
		return nil, err
	}

	entry := xml.StartElement{Name: xml.Name{Local: "url"}}

	for _, location := range ordered {
		err = encoder.EncodeElement(sitemapLocation{Loc: location}, entry)
		if err != nil {
			return nil, err
		}
	}

	err = encoder.EncodeToken(root.End())
	if err != nil {
		return nil, err
	}

	err = encoder.Close()
	if err != nil {
		return nil, err
	}

	contents := buffer.contents.Bytes()

	return &sitemapData{contents: contents, etag: contentETag(contents)}, nil
}

func sitemapPageRoutable(name, location string) (bool, error) {
	target, err := url.Parse(location)
	if err != nil {
		return false, err
	}

	if target.Path == "/sitemap.xml" {
		return false, nil
	}

	request := &http.Request{URL: target}

	response, err := resolveFrontendResponse(request)
	if response.file != nil {
		response.file.Close()
	}

	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}

	if response.info != nil && !response.info.Mode().IsRegular() {
		return false, nil
	}

	return response.redirect == "" && response.name == markdownCachePath(name), nil
}
