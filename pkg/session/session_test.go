package session

import "testing"

func TestParseURL(t *testing.T) {
	server := "https://10.49.34.24:443"
	url, _ := parseURL(server)
	println("", url.Hostname(), url.Port())
}
