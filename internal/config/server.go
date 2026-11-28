package config

import (
	"net/url"
	"regexp"
	"strings"
)

// DefaultServerURL is used by mandatory service entrances. Tests may replace
// it with an isolated HTTP server; Enabled still checks explicit configuration.
var DefaultServerURL = "http://127.0.0.1:7878"

func ServerURL(cfg *Config) string {
	if cfg != nil && cfg.Server.URL != "" {
		return cfg.Server.URL
	}
	return DefaultServerURL
}

// Server selects the board service a v2 client talks to.
type Server struct {
	URL string `json:"url,omitempty"`
}

// Project names the server project a repository binds to. Hosts with the same
// name on one server share one board.
type Project struct {
	Name string `json:"name,omitempty"`
}

// projectNamePattern copies api.ProjectNamePattern; this package cannot import
// the api package, so an api test pins the two together.
const projectNamePattern = `^[A-Za-z][A-Za-z0-9_-]{0,127}$`

var projectNameRe = regexp.MustCompile(projectNamePattern)

// ValidProjectName reports whether name satisfies the project name rule.
func ValidProjectName(name string) bool { return projectNameRe.MatchString(name) }

// validateServer permits an omitted URL; runtime service entrances resolve
// the effective default through ServerURL.
func validateServer(raw any) (Server, error) {
	obj, ok := asObject(raw)
	if !ok {
		return Server{}, configErrorf("config.server_must_be_a_json_object")
	}
	var s Server
	if value, exists := obj["url"]; exists {
		text, ok := value.(string)
		u, err := url.Parse(text)
		if !ok || err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			return Server{}, configErrorf("config.server_url_invalid")
		}
		s.URL = strings.TrimRight(text, "/")
	}
	return s, nil
}

func validateProject(raw any) (Project, error) {
	obj, ok := asObject(raw)
	if !ok {
		return Project{}, configErrorf("config.project_must_be_a_json_object")
	}
	var p Project
	if value, exists := obj["name"]; exists {
		text, ok := value.(string)
		if !ok || !ValidProjectName(text) {
			return Project{}, configErrorf("config.project_name_invalid")
		}
		p.Name = text
	}
	return p, nil
}

// Host names this host to the servers it talks to. Only the scope
// configuration sets it; see ScopeHostName.
type Host struct {
	Name string `json:"name,omitempty"`
}

// hostNamePattern copies api.HostNamePattern; an api test pins the two together.
const hostNamePattern = `^[A-Za-z0-9][A-Za-z0-9-]{0,62}$`

var hostNameRe = regexp.MustCompile(hostNamePattern)

// ValidHostName reports whether name satisfies the host name rule.
func ValidHostName(name string) bool { return hostNameRe.MatchString(name) }

func validateHost(raw any) (Host, error) {
	obj, ok := asObject(raw)
	if !ok {
		return Host{}, configErrorf("config.host_must_be_a_json_object")
	}
	var h Host
	if value, exists := obj["name"]; exists {
		text, ok := value.(string)
		if !ok || !ValidHostName(text) {
			return Host{}, configErrorf("config.host_name_invalid")
		}
		h.Name = text
	}
	return h, nil
}
