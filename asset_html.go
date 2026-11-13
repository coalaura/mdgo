package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

type assetStyleScope struct {
	styles map[string]bool
	parent int
}

func (c *AssetCatalogue) RewriteHTML(source []byte, pagePath string) ([]byte, error) {
	base, err := documentAssetBase(source, pagePath)
	if err != nil {
		return nil, err
	}

	scopes := c.linkedStyles(source, base)
	tokenizer := html.NewTokenizer(bytes.NewReader(source))

	var (
		scope     int
		nextScope int
		output    bytes.Buffer
	)

	output.Grow(len(source))

	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			err = tokenizer.Err()
			if errors.Is(err, io.EOF) {
				return output.Bytes(), nil
			}

			return nil, err
		}

		raw := tokenizer.Raw()

		if kind != html.StartTagToken && kind != html.SelfClosingTagToken && kind != html.EndTagToken {
			output.Write(raw)

			continue
		}

		token := tokenizer.Token()
		if token.Data == "template" {
			if kind == html.EndTagToken {
				scope = scopes[scope].parent
			} else {
				nextScope++
				scope = nextScope
			}
		}

		if kind == html.EndTagToken {
			output.Write(raw)

			continue
		}

		attribute := ""

		switch token.Data {
		case "script":
			if javascriptTag(token) {
				attribute = "src"
			}
		case "link":
			attribute = "href"
		}

		if attribute == "" {
			output.Write(raw)

			continue
		}

		value, exists := htmlAttribute(token, attribute)
		if !exists {
			output.Write(raw)

			continue
		}

		logical, reference := localAssetReference(value, base)

		entry, matched := c.entries[logical]
		if !matched {
			if c.scripts[logical] {
				return nil, fmt.Errorf("HTML references excluded theme script %q: add it to theme.assets.entrypoints or remove the reference", logical)
			}

			output.Write(raw)

			continue
		}

		reference.Path = "/" + entry.output
		reference.RawPath = ""
		reference.Scheme = ""
		reference.Host = ""

		setHTMLAttribute(&token, attribute, reference.String())

		if token.Data == "script" {
			scriptType, _ := htmlAttribute(token, "type")
			if strings.EqualFold(strings.TrimSpace(scriptType), "module") {
				removeHTMLAttribute(&token, "type")

				_, async := htmlAttribute(token, "async")
				_, deferred := htmlAttribute(token, "defer")

				if !async && !deferred {
					setHTMLAttribute(&token, "defer", "")
				}
			}

			if entry.css != "" {
				digest := c.outputs[entry.css].sha256
				styles := scopes[scope].styles

				if !styles[digest] {
					link := html.Token{
						Type: html.StartTagToken,
						Data: "link",
						Attr: []html.Attribute{
							{Key: "rel", Val: "stylesheet"},
							{Key: "href", Val: "/" + entry.css},
						},
					}

					output.WriteString(link.String())

					styles[digest] = true
				}
			}
		} else {
			rel, _ := htmlAttribute(token, "rel")
			if hasHTMLWord(rel, "modulepreload") {
				setHTMLAttribute(&token, "rel", replaceHTMLWord(rel, "modulepreload", "preload"))
				setHTMLAttribute(&token, "as", "script")
			}
		}

		integrity, exists := htmlAttribute(token, "integrity")
		if exists {
			setHTMLAttribute(&token, "integrity", assetIntegrity(c.outputs[entry.output], integrity))
		}

		output.WriteString(token.String())
	}
}

func (c *AssetCatalogue) linkedStyles(source []byte, base *url.URL) []assetStyleScope {
	// Each template is inert until instantiated and owns independent stylesheet state.
	scopes := []assetStyleScope{{styles: make(map[string]bool)}}
	scope := 0

	tokenizer := html.NewTokenizer(bytes.NewReader(source))

	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return scopes
		}

		if kind != html.StartTagToken && kind != html.SelfClosingTagToken && kind != html.EndTagToken {
			continue
		}

		token := tokenizer.Token()
		if token.Data == "template" {
			if kind == html.EndTagToken {
				scope = scopes[scope].parent
			} else {
				scopes = append(scopes, assetStyleScope{styles: make(map[string]bool), parent: scope})
				scope = len(scopes) - 1
			}
		}

		if kind == html.EndTagToken || token.Data != "link" {
			continue
		}

		rel, _ := htmlAttribute(token, "rel")
		media, _ := htmlAttribute(token, "media")
		_, disabled := htmlAttribute(token, "disabled")

		if !hasHTMLWord(rel, "stylesheet") || hasHTMLWord(rel, "alternate") || disabled || media != "" && !strings.EqualFold(strings.TrimSpace(media), "all") {
			continue
		}

		href, _ := htmlAttribute(token, "href")

		logical, reference := localAssetReference(href, base)
		if reference == nil {
			continue
		}

		entry, exists := c.entries[logical]
		name := strings.TrimPrefix(reference.Path, "/")

		if exists {
			name = entry.output
		}

		digests, exists := c.outputs[name]
		if exists && digests.sha256 != "" {
			scopes[scope].styles[digests.sha256] = true
		}
	}
}

func documentAssetBase(source []byte, pagePath string) (*url.URL, error) {
	base, err := url.Parse(pagePath)
	if err != nil {
		return nil, err
	}

	base.Scheme = "https"
	base.Host = "mdgo.invalid"

	tokenizer := html.NewTokenizer(bytes.NewReader(source))

	for {
		kind := tokenizer.Next()
		if kind == html.ErrorToken {
			return base, nil
		}

		if kind != html.StartTagToken && kind != html.SelfClosingTagToken {
			continue
		}

		token := tokenizer.Token()
		if token.Data != "base" {
			continue
		}

		href, exists := htmlAttribute(token, "href")
		if !exists {
			continue
		}

		reference, err := url.Parse(href)
		if err != nil {
			return base, nil
		}

		return base.ResolveReference(reference), nil
	}
}

func localAssetReference(value string, base *url.URL) (string, *url.URL) {
	reference, err := url.Parse(strings.TrimSpace(value))
	if err != nil || reference.IsAbs() || reference.Host != "" || reference.Path == "" {
		return "", nil
	}

	reference = base.ResolveReference(reference)
	if reference.Host != "mdgo.invalid" || reference.Scheme != "https" || !strings.HasPrefix(reference.Path, "/assets/") {
		return "", nil
	}

	return strings.TrimPrefix(reference.Path, "/assets/"), reference
}

func javascriptTag(token html.Token) bool {
	value, exists := htmlAttribute(token, "type")
	if !exists {
		language, _ := htmlAttribute(token, "language")
		if language != "" {
			value = "text/" + language
		}
	}

	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "module" {
		return true
	}

	value, _, _ = strings.Cut(value, ";")
	value = strings.TrimSpace(value)

	switch value {
	case "text/javascript", "application/javascript", "text/ecmascript", "application/ecmascript", "application/x-javascript", "text/jscript", "text/livescript", "text/x-javascript", "text/x-ecmascript", "text/javascript1.0", "text/javascript1.1", "text/javascript1.2", "text/javascript1.3", "text/javascript1.4", "text/javascript1.5":
		return true
	default:
		return false
	}
}

func htmlAttribute(token html.Token, name string) (string, bool) {
	for _, attribute := range token.Attr {
		if attribute.Key == name {
			return attribute.Val, true
		}
	}

	return "", false
}

func setHTMLAttribute(token *html.Token, name, value string) {
	for index := range token.Attr {
		if token.Attr[index].Key == name {
			token.Attr[index].Val = value

			return
		}
	}

	token.Attr = append(token.Attr, html.Attribute{Key: name, Val: value})
}

func removeHTMLAttribute(token *html.Token, name string) {
	attributes := token.Attr[:0]

	for _, attribute := range token.Attr {
		if attribute.Key != name {
			attributes = append(attributes, attribute)
		}
	}

	token.Attr = attributes
}

func hasHTMLWord(value, word string) bool {
	for field := range strings.FieldsSeq(value) {
		if strings.EqualFold(field, word) {
			return true
		}
	}

	return false
}

func replaceHTMLWord(value, oldWord, newWord string) string {
	var result strings.Builder

	result.Grow(len(value))

	for field := range strings.FieldsSeq(value) {
		if result.Len() != 0 {
			result.WriteByte(' ')
		}

		if strings.EqualFold(field, oldWord) {
			field = newWord
		}

		result.WriteString(field)
	}

	return result.String()
}

func assetIntegrity(digests assetDigests, previous string) string {
	var result strings.Builder

	for field := range strings.FieldsSeq(previous) {
		algorithm, _, _ := strings.Cut(field, "-")

		var digest string

		switch algorithm {
		case "sha256":
			digest = digests.sha256
		case "sha384":
			digest = digests.sha384
		case "sha512":
			digest = digests.sha512
		default:
			continue
		}

		if result.Len() != 0 {
			result.WriteByte(' ')
		}

		result.WriteString(digest)
	}

	if result.Len() == 0 {
		return digests.sha384
	}

	return result.String()
}
