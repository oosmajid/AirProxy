package main

import (
	"net"
	"syscall"
	"time"
)

// boundDialer یک Dialer می‌سازد که (در صورت داده‌شدن iface) سوکت را با IP_BOUND_IF
// به آن اینترفیس می‌بندد تا ترافیک از VPN ِ سیستمی (utun) عبور نکند.
func boundDialer(iface string, timeout time.Duration) *net.Dialer {
	d := &net.Dialer{Timeout: timeout}
	if iface == "" {
		return d
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		return d
	}
	d.Control = func(network, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			if network == "tcp6" || network == "udp6" {
				serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_BOUND_IF, ifi.Index)
			} else {
				serr = syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_BOUND_IF, ifi.Index)
			}
		})
		if err != nil {
			return err
		}
		return serr
	}
	return d
}
