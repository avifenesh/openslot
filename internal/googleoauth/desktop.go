package googleoauth

import (
	"net/url"
	"strings"
)

// Desktop OAuth may only return to this installation's loopback listener.
// It cannot authenticate a server hosted on another person's machine.
func DesktopAllowed(baseURL, bindHost, port string) bool {
	local := func(host string) bool { return host == "127.0.0.1" || host == "::1" || host == "localhost" }
	u, err := url.Parse(baseURL)
	if err != nil || u.Scheme != "http" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return false
	}
	actualPort := u.Port()
	if actualPort == "" {
		actualPort = "80"
	}
	return local(u.Hostname()) && local(bindHost) && actualPort == port
}
