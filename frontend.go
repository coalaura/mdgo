package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/coalaura/lock"
)

const (
	PublicDirectory = "public"

	generatedHTMLMarker = "<!-- mdgo:cached-page -->\n"
)

type frontendResponse struct {
	name     string
	file     *os.File
	info     fs.FileInfo
	redirect string
}

var frontendLocks = lock.NewLockMap[string]()

func LoadPublicRoot() (*os.Root, error) {
	_, err := os.Stat(PublicDirectory)
	if os.IsNotExist(err) {
		os.MkdirAll(PublicDirectory, 0755)
	}

	return os.OpenRoot(PublicDirectory)
}

func HandleFrontend(w http.ResponseWriter, r *http.Request) {
	response, err := prepareFrontendResponse(r)
	if err != nil {
		writeFrontendError(w, r, err)

		return
	}

	if response.redirect != "" {
		http.Redirect(w, r, response.redirect, http.StatusMovedPermanently)

		return
	}

	defer response.file.Close()

	http.ServeContent(w, r, response.name, response.info.ModTime(), response.file)
}

// Keep cache lookup and publication inside the invalidation barrier. The opened
// file belongs to the response after unlocking, even if a clear removes its path.
func prepareFrontendResponse(r *http.Request) (frontendResponse, error) {
	siteMutex.RLock()
	defer siteMutex.RUnlock()

	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || strings.HasSuffix(name, "/") {
		name += "index"
	}

	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		return frontendResponse{}, fs.ErrNotExist
	}

	directory, assetName, _ := strings.Cut(name, "/")

	isAsset := strings.EqualFold(strings.TrimRight(directory, ". "), "assets")
	if isAsset {
		assetName, _, _ = strings.Cut(assetName, "/")
		if strings.EqualFold(strings.TrimRight(assetName, ". "), ".manifest") {
			return frontendResponse{}, fs.ErrNotExist
		}
	}

	redirect, err := frontendRedirect(r, name)
	if err != nil || redirect != "" {
		return frontendResponse{redirect: redirect}, err
	}

	file, info, err := openFrontendFile(name)
	if info != nil && info.IsDir() && !strings.HasSuffix(r.URL.Path, "/") {
		target := url.URL{
			Path:       r.URL.Path + "/",
			RawQuery:   r.URL.RawQuery,
			ForceQuery: r.URL.ForceQuery,
		}

		return frontendResponse{redirect: target.RequestURI()}, nil
	} else if errors.Is(err, fs.ErrNotExist) && !strings.HasSuffix(name, ".html") {
		name += ".html"

		file, info, err = openFrontendFile(name)
	}

	if errors.Is(err, fs.ErrNotExist) {
		file, info, err = renderFrontendFile(name, r.URL.EscapedPath())
	}

	if err != nil {
		return frontendResponse{}, err
	}

	return frontendResponse{name: name, file: file, info: info}, nil
}

func frontendRedirect(r *http.Request, name string) (string, error) {
	extension := path.Ext(name)

	isMarkdown := strings.EqualFold(extension, ".md")
	isHTML := strings.EqualFold(extension, ".html")

	targetPath := r.URL.Path

	if isMarkdown || isHTML {
		targetPath = strings.TrimSuffix(targetPath, extension)
	}

	redirectIndex := config.Frontend.RedirectIndex && strings.HasSuffix(targetPath, "/index")
	if redirectIndex {
		targetPath = strings.TrimSuffix(targetPath, "index")
	} else if !isMarkdown && (!isHTML || !config.Frontend.RedirectHTML) {
		return "", nil
	}

	info, err := public.Stat(name)

	if !isMarkdown && !isHTML {
		missingFile := errors.Is(err, fs.ErrNotExist) || err == nil && !info.Mode().IsRegular()
		if missingFile {
			info, err = public.Stat(name + ".html")
		}
	}

	if !isMarkdown && errors.Is(err, fs.ErrNotExist) {
		info, err = public.Stat(strings.TrimSuffix(name, extension) + ".md")
	}

	if err != nil {
		return "", err
	}

	if !info.Mode().IsRegular() {
		return "", fs.ErrNotExist
	}

	target := url.URL{Path: targetPath}
	location := target.EscapedPath()

	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		location += "?" + r.URL.RawQuery
	}

	return location, nil
}

func openFrontendFile(name string) (*os.File, fs.FileInfo, error) {
	file, err := public.Open(name)
	if err != nil {
		return nil, nil, err
	}

	info, err := file.Stat()
	if err != nil {
		file.Close()

		return nil, nil, err
	}

	if !info.Mode().IsRegular() {
		file.Close()

		return nil, info, fs.ErrNotExist
	}

	return file, info, nil
}

func renderFrontendFile(name, pagePath string) (*os.File, fs.FileInfo, error) {
	markdownName := strings.TrimSuffix(name, ".html") + ".md"

	_, err := public.Stat(markdownName)
	if err != nil {
		return nil, nil, err
	}

	frontendLocks.Lock(name)
	defer frontendLocks.Unlock(name)

	file, info, err := openFrontendFile(name)
	if info != nil || !errors.Is(err, fs.ErrNotExist) {
		return file, info, err
	}

	err = writeFrontendHTML(name, markdownName, pagePath)
	if err != nil {
		return nil, nil, err
	}

	return openFrontendFile(name)
}

func writeFrontendHTML(name, markdownName, pagePath string) error {
	source, err := public.ReadFile(markdownName)
	if err != nil {
		return err
	}

	source, meta, err := ParseMarkdownMetadata(source)
	if err != nil {
		return err
	}

	temporaryName := path.Join(path.Dir(name), ".mdgo-"+rand.Text()+".tmp")

	file, err := public.OpenFile(temporaryName, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if err != nil {
		return err
	}

	defer public.Remove(temporaryName)
	defer file.Close()

	var markdown bytes.Buffer

	markdown.Grow(len(source))

	document := frontendParser.Parse(source)

	err = frontendRenderer.Render(&markdown, source, document)
	if err != nil {
		return err
	}

	var rendered bytes.Buffer

	err = theme.Render(&rendered, markdown.Bytes(), meta)
	if err != nil {
		return err
	}

	completed, err := theme.catalogue.RewriteHTML(rendered.Bytes(), pagePath)
	if err != nil {
		return err
	}

	_, err = file.WriteString(generatedHTMLMarker)
	if err != nil {
		return err
	}

	_, err = file.Write(completed)
	if err != nil {
		return err
	}

	err = file.Close()
	if err != nil {
		return err
	}

	return public.Rename(temporaryName, name)
}

func writeFrontendError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	if errors.Is(err, fs.ErrNotExist) {
		http.NotFound(w, r)

		return
	}

	if errors.Is(err, fs.ErrPermission) {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)

		return
	}

	log.Warnln(err)

	http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
}
