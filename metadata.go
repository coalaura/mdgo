package main

import (
	"bytes"
	"errors"

	"github.com/goccy/go-yaml"
)

type Metadata map[string]any

func ParseMarkdownMetadata(markdown []byte) ([]byte, Metadata, error) {
	if len(markdown) == 0 {
		return markdown, nil, nil
	}

	var metadataStart int

	for lineStart := 0; lineStart < len(markdown); {
		newline := bytes.IndexByte(markdown[lineStart:], '\n')

		lineEnd := len(markdown)
		nextLine := len(markdown)

		if newline >= 0 {
			lineEnd = lineStart + newline
			nextLine = lineEnd + 1
		}

		line := markdown[lineStart:lineEnd]
		if newline >= 0 && len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}

		if lineStart == 0 {
			if !isMetadataMarker(line) {
				return markdown, nil, nil
			}

			metadataStart = nextLine
		} else if isMetadataMarker(line) {
			var meta Metadata

			metadata := markdown[metadataStart:lineStart]
			if len(bytes.Trim(metadata, " \t\r\n")) > 0 {
				err := yaml.Unmarshal(metadata, &meta)
				if err != nil {
					return nil, nil, err
				}
			}

			return markdown[nextLine:], meta, nil
		}

		lineStart = nextLine
	}

	return nil, nil, errors.New("missing closing metadata marker")
}

func isMetadataMarker(line []byte) bool {
	return len(line) >= 3 && bytes.Equal([]byte("---"), bytes.TrimRight(line, " \t"))
}
