package app

import (
	"strings"

	"github.com/egoist/mygo/ui/editor"
)

// configEditor keeps exact source bytes alongside the editor's LF-normalized
// buffer. Only the changed span acquires the document's preferred line ending.
type configEditor struct {
	state      editor.State
	original   string
	normalized string
	ending     string
}

func newConfigEditor(text string) *configEditor {
	b := editor.NewBuffer(text)
	b.SetLanguage(editor.LanguageFor("network.toml"))
	return &configEditor{state: editor.State{Buffer: b, Label: "TOML 配置编辑器"}, original: text, normalized: b.String(), ending: b.LineEnding()}
}

func (e *configEditor) Modified() bool { return e.state.Buffer.Modified() }

func (e *configEditor) String() string {
	current := e.state.Buffer.String()
	if current == e.normalized {
		return e.original
	}
	start := 0
	for start < len(current) && start < len(e.normalized) && current[start] == e.normalized[start] {
		start++
	}
	oldEnd, newEnd := len(e.normalized), len(current)
	for oldEnd > start && newEnd > start && e.normalized[oldEnd-1] == current[newEnd-1] {
		oldEnd--
		newEnd--
	}
	// Map normalized byte offsets back to the original, retaining CRLF and
	// mixed line endings outside the changed span exactly.
	rawOffset := func(offset int) int {
		raw, normalized := 0, 0
		for normalized < offset {
			if e.original[raw] == '\r' && raw+1 < len(e.original) && e.original[raw+1] == '\n' {
				raw++
			}
			raw++
			normalized++
		}
		return raw
	}
	changed := current[start:newEnd]
	if e.ending == "\r\n" {
		changed = strings.ReplaceAll(changed, "\n", "\r\n")
	}
	return e.original[:rawOffset(start)] + changed + e.original[rawOffset(oldEnd):]
}
