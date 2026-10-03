// Package chunk splits a script into sentence-sized pieces that a TTS model
// can render independently and in parallel.
package chunk

import (
	"strings"
	"unicode/utf8"
)

// Split breaks text into chunks of whole sentences, merging consecutive
// sentences while the chunk stays within target runes. A sentence longer than
// hardCap is cut at its last comma, then its last space, before hardCap; a
// single word longer than hardCap is cut hard. Chunks are trimmed and never
// empty; their order follows the text. Joining the chunks with a single space
// reproduces the input's words in order (hard cuts excepted).
func Split(text string, target, hardCap int) []string {
	target = min(target, hardCap)
	var out []string
	cur := ""
	flush := func() {
		if s := strings.TrimSpace(cur); s != "" {
			out = append(out, s)
		}
		cur = ""
	}
	for _, s := range sentences(text) {
		for _, piece := range capPieces(s, hardCap) {
			switch {
			case cur == "":
				cur = piece
			case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(piece) <= target:
				cur += " " + piece
			default:
				flush()
				cur = piece
			}
		}
	}
	flush()
	return out
}

func sentences(text string) []string {
	var out []string
	var b strings.Builder
	runes := []rune(text)
	boundary := func(i int) bool {
		return i >= len(runes) || runes[i] == ' ' || runes[i] == '\n'
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\n' {
			out = appendTrimmed(out, b.String())
			b.Reset()
			continue
		}
		b.WriteRune(r)
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		switch {
		case boundary(i + 1):
		case runes[i+1] == '"' && boundary(i+2):
			b.WriteRune('"')
			i++
		default:
			continue
		}
		out = appendTrimmed(out, b.String())
		b.Reset()
	}
	return appendTrimmed(out, b.String())
}

func appendTrimmed(out []string, s string) []string {
	if s = strings.TrimSpace(s); s != "" {
		out = append(out, s)
	}
	return out
}

func capPieces(s string, hardCap int) []string {
	var out []string
	for utf8.RuneCountInString(s) > hardCap {
		r := []rune(s)
		head := string(r[:hardCap])
		cut := lastClauseComma(s, len(head))
		if cut <= 0 {
			cut = strings.LastIndex(head, " ")
		}
		if cut <= 0 {
			out = append(out, head)
			s = string(r[hardCap:])
			continue
		}
		out = appendTrimmed(out, s[:cut+1])
		s = strings.TrimSpace(s[cut+1:])
	}
	return appendTrimmed(out, s)
}

func lastClauseComma(s string, headLen int) int {
	for i := strings.LastIndex(s[:headLen], ","); i > 0; i = strings.LastIndex(s[:i], ",") {
		if i+1 >= len(s) || s[i+1] == ' ' {
			return i
		}
	}
	return -1
}
