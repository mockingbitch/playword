// Package registry names the dictionary sources the server and CLI can
// ingest.
package registry

import (
	"slices"

	"playword/internal/adapters/sources/hnd"
	"playword/internal/adapters/sources/oewn"
	"playword/internal/adapters/sources/wiktionary"
	"playword/internal/ports"
)

// Dictionaries returns every dictionary source by name.
func Dictionaries() map[string]ports.Source {
	return map[string]ports.Source{"hnd": hnd.New("", nil), "oewn": oewn.New("", nil), "wiktionary-vi": wiktionary.New("", nil)}
}

// Names lists the dictionary sources, sorted.
func Names() []string {
	var out []string
	for n := range Dictionaries() {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}
