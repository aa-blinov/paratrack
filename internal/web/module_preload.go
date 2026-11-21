package web

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
)

// modulePreloads lists, per UI language, the chunks app.js imports statically
// plus that language's dictionary. Without the links the browser found them
// one round trip at a time: the page, then app.js, then its imports, then the
// dictionary the loader awaits, ~920 ms to a drawn login screen at 80 ms RTT.
var modulePreloads, modulePreloadsErr = readModulePreloads(assets)

type viteChunk struct {
	File    string   `json:"file"`
	Imports []string `json:"imports"`
}

func readModulePreloads(source fs.FS) (map[string][]string, error) {
	raw, err := fs.ReadFile(source, "static/ui/manifest.json")
	if err != nil {
		return nil, fmt.Errorf("read vite manifest: %w", err)
	}
	var manifest map[string]viteChunk
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return nil, fmt.Errorf("parse vite manifest: %w", err)
	}
	seen := map[string]bool{}
	var walk func(key string)
	walk = func(key string) {
		for _, dep := range manifest[key].Imports {
			if !seen[dep] {
				seen[dep] = true
				walk(dep)
			}
		}
	}
	walk("src/main.tsx")
	var shared []string
	for key := range seen {
		shared = append(shared, "/static/ui/"+manifest[key].File)
	}
	sort.Strings(shared)
	out := map[string][]string{}
	for _, lang := range []string{"ru", "en"} {
		dict, ok := manifest["../internal/i18n/dict/"+lang+".json"]
		if !ok {
			return nil, fmt.Errorf("vite manifest has no %s dictionary", lang)
		}
		out[lang] = append(append([]string{}, shared...), "/static/ui/"+dict.File)
	}
	return out, nil
}
