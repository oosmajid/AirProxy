package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/xtls/xray-core/infra/conf/serial"
)

func TestExtractGeoToWritesFiles(t *testing.T) {
	dir := t.TempDir()
	if err := extractGeoTo(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"geoip.dat", "geosite.dat"} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s missing: %v", name, err)
		}
		if fi.Size() == 0 {
			t.Fatalf("%s is empty", name)
		}
	}
}

// اثبات اینکه فایل‌های bundle‌شده واقعاً برای Xray قابل بارگذاری‌اند و شامل
// دسته‌های geoip:ir / geosite:category-ir / geoip:private هستند.
func TestBundledGeoLoadsInXray(t *testing.T) {
	dir := t.TempDir()
	if err := extractGeoTo(dir); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XRAY_LOCATION_ASSET", dir)

	cfg := buildConfig("127.0.0.1", 10808, 0,
		map[string]interface{}{"tag": "proxy", "protocol": "freedom"},
		buildRouting(bypassRules{Enabled: true, Iran: true, Private: true, BlockQUIC: true}),
	)
	jsonBytes, _ := json.Marshal(cfg)
	if _, err := serial.LoadJSONConfig(bytes.NewReader(jsonBytes)); err != nil {
		t.Fatalf("xray failed to load bundled geo (geoip:ir / geosite:category-ir): %v", err)
	}
}
