// Package servicectl controls a local server without importing its storage or
// HTTP implementation. Both the command runner and remote sessions use it.
package servicectl

import (
	"github.com/dualface/kander/internal/api"
	"net"
	"net/url"
	"os"
	"path/filepath"
)

func DataDir(value string) (string, error) {
	if value != "" {
		return filepath.Abs(value)
	}
	if xdg := os.Getenv("XDG_DATA_HOME"); filepath.IsAbs(xdg) {
		return filepath.Join(xdg, "kander-server"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "kander-server"), nil
}

func Local(target string) bool {
	u, err := url.Parse(target)
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "127.0.0.1", "localhost", "::1":
		return true
	}
	return false
}

func ListenAddress(target string) string {
	if !Local(target) {
		return api.DefaultListen
	}
	u, _ := url.Parse(target)
	port := u.Port()
	if port == "" {
		if u.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}
	return net.JoinHostPort(u.Hostname(), port)
}

func ProbeURL(listen string) (string, error) {
	host, port, err := net.SplitHostPort(listen)
	if err != nil {
		return "", err
	}
	switch host {
	case "", "0.0.0.0":
		host = "127.0.0.1"
	case "::":
		host = "::1"
	}
	return "http://" + net.JoinHostPort(host, port), nil
}
