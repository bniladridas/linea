package tui

import (
	"strings"
	"unicode"
)

func replaceDelimited(line string, delimiter string, render func(string) string) string {
	var b strings.Builder
	for {
		start := strings.Index(line, delimiter)
		if start < 0 {
			b.WriteString(line)
			break
		}
		end := strings.Index(line[start+len(delimiter):], delimiter)
		if end < 0 {
			b.WriteString(line)
			break
		}
		b.WriteString(line[:start])
		valueStart := start + len(delimiter)
		valueEnd := valueStart + end
		b.WriteString(render(line[valueStart:valueEnd]))
		line = line[valueEnd+len(delimiter):]
	}
	return b.String()
}

func replaceLinks(line string, t theme) string {
	var b strings.Builder
	for {
		start := strings.Index(line, "[")
		if start < 0 {
			b.WriteString(line)
			break
		}
		endBracket := strings.Index(line[start+1:], "](")
		if endBracket < 0 {
			b.WriteString(line)
			break
		}
		endParen := strings.Index(line[start+1+endBracket+2:], ")")
		if endParen < 0 {
			b.WriteString(line)
			break
		}
		text := line[start+1 : start+1+endBracket]
		url := line[start+1+endBracket+2 : start+1+endBracket+2+endParen]
		b.WriteString(line[:start])
		b.WriteString(t.accent(text))
		b.WriteString(t.muted(" (" + url + ")"))
		line = line[start+1+endBracket+2+endParen+1:]
	}
	return b.String()
}

func isOrderedMarkdownLine(line string) bool {
	index := 0
	for index < len(line) && unicode.IsDigit(rune(line[index])) {
		index++
	}
	return index > 0 && index+1 < len(line) && line[index] == '.' && unicode.IsSpace(rune(line[index+1]))
}

func splitOrderedMarkdownLine(line string) (string, string) {
	number, rest, _ := strings.Cut(line, ".")
	return number, strings.TrimSpace(rest)
}
