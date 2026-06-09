package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// ثبت همهٔ فیچرها (inbound/outbound handlers, transports, ...)
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Engine یک نمونهٔ در حال اجرای Xray را مدیریت می‌کند.
type Engine struct {
	mu       sync.Mutex
	instance *core.Instance
	listen   string
	socks    int
}

// Start با یک لینک، پروکسی را روی listen/socks/http بالا می‌آورد.
// اگر از قبل چیزی در حال اجرا باشد، ابتدا متوقف می‌شود.
func (e *Engine) Start(link, listen string, socksPort, httpPort int) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.instance != nil {
		e.instance.Close()
		e.instance = nil
	}

	outbound, err := parseLink(link)
	if err != nil {
		return fmt.Errorf("parse config: %w", err)
	}

	cfg := buildConfig(listen, socksPort, httpPort, outbound)
	jsonBytes, _ := json.MarshalIndent(cfg, "", "  ")

	coreCfg, err := serial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	inst, err := core.New(coreCfg)
	if err != nil {
		return fmt.Errorf("create core: %w", err)
	}
	if err := inst.Start(); err != nil {
		return fmt.Errorf("start core: %w", err)
	}
	e.instance = inst
	e.listen = listen
	e.socks = socksPort
	return nil
}

// Stop نمونهٔ در حال اجرا را متوقف می‌کند.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.instance != nil {
		e.instance.Close()
		e.instance = nil
	}
}

// Running وضعیت اجرا را برمی‌گرداند.
func (e *Engine) Running() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.instance != nil
}

// Endpoint آدرس SOCKS فعلی را برمی‌گرداند.
func (e *Engine) Endpoint() (string, int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.listen, e.socks
}
