package web

import (
	"testing"
	"testing/fstest"
)

func TestComputeAssetVersionUsesAllStaticFiles(t *testing.T) {
	first := fstest.MapFS{
		"static/ui/app.js":   &fstest.MapFile{Data: []byte("boot")},
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
