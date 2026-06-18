package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/xtls/xray-core/core"
	"github.com/xtls/xray-core/infra/conf/serial"

	// ثبت همهٔ فیچرها (inbound/outbound handlers, transports, ...)
	_ "github.com/xtls/xray-core/main/distro/all"
)

// Engine یک نمونهٔ در حال اجرای Xray را مدیریت می‌کند.
// برای لینک‌های ssh:// یک تونل SSH→SOCKS5 محلی بالا می‌آید و xray از طریق
// یک اوت‌باند socks به آن وصل می‌شود تا همهٔ امکانات inbound یکسان بماند.
type Engine struct {
	mu       sync.Mutex
	instance *core.Instance
	ssh      *sshTunnel
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
	e.cleanupSSH()

	var outbound map[string]interface{}
	if strings.HasPrefix(strings.TrimSpace(link), "ssh://") {
		tun, port, err := startSSHTunnel(link)
		if err != nil {
			return fmt.Errorf("ssh tunnel: %w", err)
		}
		e.ssh = tun
		outbound = sshOutbound(port)
	} else {
		ob, err := parseLink(link)
		if err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
		outbound = ob
	}

	cfg := buildConfig(listen, socksPort, httpPort, outbound)
	jsonBytes, _ := json.MarshalIndent(cfg, "", "  ")

	coreCfg, err := serial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		e.cleanupSSH()
		return fmt.Errorf("load config: %w", err)
	}
	inst, err := core.New(coreCfg)
	if err != nil {
		e.cleanupSSH()
		return fmt.Errorf("create core: %w", err)
	}
	if err := inst.Start(); err != nil {
		e.cleanupSSH()
		return fmt.Errorf("start core: %w", err)
	}
	e.instance = inst
	e.listen = listen
	e.socks = socksPort
	return nil
}

// cleanupSSH تونل SSH در حال اجرا را می‌بندد (باید با قفل گرفته‌شده صدا زده شود).
func (e *Engine) cleanupSSH() {
	if e.ssh != nil {
		e.ssh.Close()
		e.ssh = nil
	}
}

// Stop نمونهٔ در حال اجرا را متوقف می‌کند.
func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.instance != nil {
		e.instance.Close()
		e.instance = nil
	}
	e.cleanupSSH()
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
