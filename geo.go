package main

import (
	_ "embed"
	"os"
	"path/filepath"
)

//go:embed assets/geo/geoip.dat
var geoipDat []byte

//go:embed assets/geo/geosite.dat
var geositeDat []byte

// ensureGeoAssets فایل‌های geo را در پوشهٔ پشتیبان اپ می‌نویسد (اگر نبود/تغییر
// اندازه داشت) و XRAY_LOCATION_ASSET را به آن پوشه اشاره می‌دهد. مسیر را برمی‌گرداند.
func ensureGeoAssets() (string, error) {
	dir, err := geoDir()
	if err != nil {
		return "", err
	}
	if err := extractGeoTo(dir); err != nil {
		return "", err
	}
	// Xray هم "xray.location.asset" و هم فرم نرمال‌شدهٔ XRAY_LOCATION_ASSET را می‌خواند.
	os.Setenv("XRAY_LOCATION_ASSET", dir)
	return dir, nil
}

func geoDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "AirProxy", "geo"), nil
}

// extractGeoTo هر دو فایل embed‌شده را در dir می‌نویسد (idempotent).
func extractGeoTo(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := writeIfChanged(filepath.Join(dir, "geoip.dat"), geoipDat); err != nil {
		return err
	}
	return writeIfChanged(filepath.Join(dir, "geosite.dat"), geositeDat)
}

// writeIfChanged فایل را فقط وقتی می‌نویسد که موجود نباشد یا اندازه‌اش فرق کند.
func writeIfChanged(path string, data []byte) error {
	if fi, err := os.Stat(path); err == nil && fi.Size() == int64(len(data)) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}
