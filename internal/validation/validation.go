package validation

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"
)

var dangerousPatterns = regexp.MustCompile(`(?i)\b(eval|Function|fetch|require|process|import|export|__proto__|constructor|prototype)\b`)

func ValidateURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
	default:
		return fmt.Errorf("URL scheme '%s' is not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "0.0.0.0" {
		return fmt.Errorf("access to localhost is not allowed")
	}
	if ip := net.ParseIP(host); ip != nil && isPrivateIP(ip) {
		return fmt.Errorf("access to private IP %s is not allowed", host)
	}
	return nil
}

func isPrivateIP(ip net.IP) bool {
	ranges := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "169.254.0.0/16", "::1/128", "fc00::/7", "fe80::/10"}
	for _, cidr := range ranges {
		_, network, _ := net.ParseCIDR(cidr)
		if network != nil && network.Contains(ip) {
			return true
		}
	}
	return false
}

func ValidateSelector(selector string) error {
	if selector == "" {
		return fmt.Errorf("selector cannot be empty")
	}
	if len(selector) > 1000 {
		return fmt.Errorf("selector is too long (max 1000 chars)")
	}
	return nil
}

func ValidateScript(script string) error {
	if script == "" {
		return fmt.Errorf("script cannot be empty")
	}
	if len(script) > 100000 {
		return fmt.Errorf("script is too long (max 100000 chars)")
	}
	matches := dangerousPatterns.FindAllString(script, -1)
	if len(matches) > 0 {
		return fmt.Errorf("script contains potentially dangerous patterns: %s", strings.Join(matches, ", "))
	}
	return nil
}

func ValidateCoordinates(x, y float64) error {
	if x < 0 || y < 0 {
		return fmt.Errorf("coordinates must be non-negative, got x=%f, y=%f", x, y)
	}
	if x > 100000 || y > 100000 {
		return fmt.Errorf("coordinates out of reasonable range, got x=%f, y=%f", x, y)
	}
	return nil
}

func ValidateMouseButton(button string) error {
	switch button {
	case "left", "right", "middle", "back", "forward", "":
		return nil
	default:
		return fmt.Errorf("invalid mouse button: %s (must be left, right, middle, back, or forward)", button)
	}
}
