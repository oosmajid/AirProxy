//go:build !darwin

package main

import (
	"net"
	"time"
)

func boundDialer(_ string, timeout time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout}
}
