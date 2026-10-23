package main

import (
	"bufio"
	"crypto/rand"
	"errors"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"

	"github.com/coalaura/lock"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
)

const PublicDirectory = "public"

var (
	frontendLocks    = lock.NewLockMap[string]()
	frontendParser   = parser.New()
	frontendRenderer = html.New()
)

func LoadPublicRoot() (*os.Root, error) {
	_, err := os.Stat(PublicDirectory)
	if os.IsNotExist(err) {
		os.MkdirAll(PublicDirectory, 0755)
	}

	return os.OpenRoot(PublicDirectory)
}

func HandleFrontend(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" || strings.HasSuffix(name, "/") {
		name += "index"
	}

	if !fs.ValidPath(name) || strings.ContainsAny(name, "\\:") {
		http.NotFound(w, r)

		return
	}

	if handleFrontendRedirect(w, r, name) {
		return
	}

	file, info, err := openFrontendFile(name)
	if errors.Is(err, fs.ErrNotExist) && !strings.HasSuffix(name, ".html") {
		name += ".html"

		file, info, err = openFrontendFile(name)
	}

	if errors.Is(err, fs.ErrNotExist) {
		file, info, err = renderFrontendFile(name)
	}

	if err != nil {
		writeFrontendError(w, r, err)

		return
	}

	defer file.Close()

	http.ServeContent(w, r, name, info.ModTime(), file)
}

func handleFrontendRedirect(w http.ResponseWriter, r *http.Request, name string) bool {
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
		return false
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
		writeFrontendError(w, r, err)

		return true
	}

	if !info.Mode().IsRegular() {
		http.NotFound(w, r)

		return true
	}

	target := url.URL{Path: targetPath}
	location := target.EscapedPath()

	if r.URL.ForceQuery || r.URL.RawQuery != "" {
		location += "?" + r.URL.RawQuery
	}

	http.Redirect(w, r, location, http.StatusMovedPermanently)

	return true
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

func renderFrontendFile(name string) (*os.File, fs.FileInfo, error) {
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

	err = writeFrontendHTML(name, markdownName)
	if err != nil {
		return nil, nil, err
	}

	return openFrontendFile(name)
}

func writeFrontendHTML(name, markdownName string) error {
	source, err := public.ReadFile(markdownName)
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

	writer := bufio.NewWriter(file)
	document := frontendParser.Parse(source)

	err = frontendRenderer.Render(writer, source, document)
	if err != nil {
		return err
	}

	err = writer.Flush()
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
