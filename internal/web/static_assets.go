package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"sync"
)

//go:embed templates/*.html static/*
var assets embed.FS

// assetVersion is a hash of every embedded static file. It busts the
// browser cache (?v= on asset URLs) and names the service-worker cache.
var assetVersion, assetVersionErr = computeAssetVersion(assets)

func computeAssetVersion(source fs.FS) (string, error) {
	h := sha256.New()
	files := 0
	err := fs.WalkDir(source, "static", func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !d.IsDir() {
			b, err := fs.ReadFile(source, p)
			if err != nil {
				return fmt.Errorf("read embedded asset %s: %w", p, err)
			}
			_, _ = h.Write([]byte(p))
			_, _ = h.Write([]byte{0})
			_, _ = h.Write(b)
			_, _ = h.Write([]byte{0})
			files++
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("walk embedded static assets: %w", err)
	}
	if files == 0 {
		return "", fmt.Errorf("no embedded static assets")
	}
	return hex.EncodeToString(h.Sum(nil))[:10], nil
}

var appImportMapCache struct {
	once  sync.Once
	value template.HTML
	err   error
}

type appImportMapResponse struct {
	Imports map[string]string `json:"imports"`
}

// appImportMap includes every embedded frontend module so new local imports
// receive the same content hash as ordinary asset URLs without a second
// hand-maintained module list.
func appImportMap() (template.HTML, error) {
	appImportMapCache.once.Do(func() {
		if assetVersionErr != nil {
			appImportMapCache.err = fmt.Errorf("hash embedded static assets: %w", assetVersionErr)
			return
		}
		imports := map[string]string{}
		modules, err := fs.Glob(assets, "static/js/*.js")
		if err != nil {
			appImportMapCache.err = fmt.Errorf("list frontend modules: %w", err)
			return
		}
		if len(modules) == 0 {
			appImportMapCache.err = fmt.Errorf("no frontend modules embedded")
			return
		}
		for _, file := range modules {
			urlPath := "/" + file
			imports[urlPath] = urlPath + "?v=" + assetVersion
		}
		data, err := json.Marshal(appImportMapResponse{Imports: imports})
		if err != nil {
			appImportMapCache.err = fmt.Errorf("encode frontend import map: %w", err)
			return
		}
		// The browser expects raw JSON inside <script type="importmap">.
		// The bytes come from json.Marshal over embedded asset paths and the
		// content hash, so they are safe to emit without HTML entity escaping.
		appImportMapCache.value = template.HTML(data)
	})
	return appImportMapCache.value, appImportMapCache.err
}
