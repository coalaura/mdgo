package main

import (
	"github.com/yuin/goldmark/v2/ast"
	"github.com/yuin/goldmark/v2/extension"
	"github.com/yuin/goldmark/v2/parser"
	"github.com/yuin/goldmark/v2/renderer/html"
	"github.com/yuin/goldmark/v2/text"
	"github.com/yuin/goldmark/v2/util"
)

type MarkdownAlertTransformer struct{}

var (
	frontendParser   = NewFrontendParser()
	frontendRenderer = html.New(
		html.WithUnsafe(),
		html.WithExtensions(
			extension.NewGFMHTMLRenderer(),
			extension.NewFootnoteHTMLRenderer(),
			extension.NewDefinitionListHTMLRenderer(),
		),
	)
)

func (MarkdownAlertTransformer) Transform(paragraph *ast.Paragraph, reader text.Reader, _ parser.Context) {
	quote, ok := paragraph.Parent().(*ast.Blockquote)
	if !ok || quote.FirstChild() != paragraph {
		return
	}

	lines := paragraph.Source()
	if len(lines) == 0 {
		return
	}

	markerLine := lines[0].TrimLeftSpace(reader.Source()).TrimRightSpace(reader.Source())
	marker := markerLine.Str(reader.Source())

	var alertType string

	switch marker {
	case "[!NOTE]":
		alertType = "note"
	case "[!TIP]":
		alertType = "tip"
	case "[!IMPORTANT]":
		alertType = "important"
	case "[!WARNING]":
		alertType = "warning"
	case "[!CAUTION]":
		alertType = "caution"
	default:
		return
	}

	class := "markdown-alert markdown-alert-" + alertType
	quote.SetAttribute("class", text.NewMultiLineValueFromString(class, text.IdentityDecoder))

	heading := ast.NewParagraph()
	heading.SetAttribute("class", text.NewMultiLineValueFromString("markdown-alert-title", text.IdentityDecoder))

	markerLine.Start += 2
	markerLine.Stop--

	heading.SetSource([]text.Segment{markerLine})
	quote.InsertBefore(paragraph, heading)

	paragraph.SetSource(lines[1:])

	if len(lines) == 1 {
		quote.RemoveChild(paragraph)
	}
}

func NewFrontendParser() parser.Parser {
	extensions := parser.WithExtensions(extension.NewGFMParser(), extension.NewFootnoteParser(), extension.NewDefinitionListParser())
	alerts := parser.WithParagraphTransformers(util.Prioritized[parser.ParagraphTransformer](MarkdownAlertTransformer{}, 50))

	return parser.New(parser.WithAutoHeadingID(), extensions, alerts)
}
