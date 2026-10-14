package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
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
