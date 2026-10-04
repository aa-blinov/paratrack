package web

import (
	"encoding/json"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

var testAppModuleReference = regexp.MustCompile(`(?m)(?:\b(?:import|export)\s+(?:[^"']*?\s+from\s+)?|\bimport\s*\()\s*["']([^"']+)["']`)

func TestComputeAssetVersionUsesAllStaticFiles(t *testing.T) {
	first := fstest.MapFS{
		"static/js/app.js":   &fstest.MapFile{Data: []byte("boot")},
		"static/css/app.css": &fstest.MapFile{Data: []byte("style")},
	}
	version, err := computeAssetVersion(first)
	if err != nil {
		t.Fatalf("compute asset version: %v", err)
	}
	versionAgain, err := computeAssetVersion(first)
	if err != nil {
		t.Fatalf("compute asset version again: %v", err)
	}
	if versionAgain != version {
		t.Fatalf("asset version is not stable: got %q, want %q", versionAgain, version)
	}
	first["static/css/app.css"] = &fstest.MapFile{Data: []byte("changed")}
	changedVersion, err := computeAssetVersion(first)
	if err != nil {
		t.Fatalf("compute changed asset version: %v", err)
	}
	if changedVersion == version {
		t.Fatal("asset content change did not change the cache version")
	}
}

func TestComputeAssetVersionRejectsMissingStaticAssets(t *testing.T) {
	if _, err := computeAssetVersion(fstest.MapFS{}); err == nil {
		t.Fatal("compute asset version succeeded without an embedded static directory")
	}
}

func TestAppImportMapCoversEmbeddedModuleGraph(t *testing.T) {
	var importMap struct {
		Imports map[string]string `json:"imports"`
	}
	encoded, err := appImportMap()
	if err != nil {
		t.Fatalf("build import map: %v", err)
	}
	if err := json.Unmarshal([]byte(encoded), &importMap); err != nil {
		t.Fatalf("decode import map: %v", err)
	}

	modules, err := fs.Glob(assets, "static/js/*.js")
	if err != nil {
		t.Fatalf("list embedded modules: %v", err)
	}
	for _, file := range modules {
		if _, err := fs.Stat(assets, file); err != nil {
			t.Errorf("module %s is not embedded: %v", file, err)
			continue
		}
	}
	appModules, err := fs.Glob(assets, "static/js/app*.js")
	if err != nil {
		t.Fatalf("list app modules: %v", err)
	}
	for _, file := range appModules {
		source, err := assets.ReadFile(file)
		if err != nil {
			t.Errorf("read embedded module %s: %v", file, err)
			continue
		}
		for _, match := range testAppModuleReference.FindAllSubmatch(source, -1) {
			urlPath := string(match[1])
			if !strings.HasPrefix(urlPath, "/static/js/") || !strings.HasSuffix(urlPath, ".js") {
				t.Errorf("unsupported frontend module specifier %q in %s", urlPath, file)
				continue
			}
			mappedURL, ok := importMap.Imports[urlPath]
			if !ok {
				t.Errorf("module %s is absent from import map", urlPath)
				continue
			}
			if mappedURL != urlPath+"?v="+assetVersion {
				t.Errorf("module %s has unexpected cache URL %q", urlPath, mappedURL)
			}
		}
	}
	if len(importMap.Imports) != len(modules) {
		t.Fatalf("import map has %d entries for %d embedded modules", len(importMap.Imports), len(modules))
	}
	for _, file := range modules {
		urlPath := "/" + file
		if _, ok := importMap.Imports[urlPath]; !ok {
			t.Errorf("embedded module %s is absent from import map", file)
		}
	}
}
