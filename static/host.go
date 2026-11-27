package static

import (
	"net"
	"strings"
)

// validHost prevents DNS rebinding against a loopback listener. An explicitly
// non-loopback deployment may use service DNS names or public authorities.
func validHost(bound, authority string) bool {
	host, port, err := net.SplitHostPort(bound)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if !ip.IsLoopback() {
		return true
	}
	requestHost, requestPort, err := net.SplitHostPort(authority)
	if err != nil {
		if port != "80" {
			return false
		}
		requestHost, requestPort = authority, "80"
		if strings.HasPrefix(authority, "[") && strings.HasSuffix(authority, "]") {
			requestHost = authority[1 : len(authority)-1]
		}
	}
	return requestPort == port && (strings.EqualFold(requestHost, "localhost") || ip.Equal(net.ParseIP(requestHost)))
}
