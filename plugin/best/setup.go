//go:generate go run owners_generate.go

package best

import (
	"net"
	"sort"

	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"

	"github.com/mholt/caddy"
)

func init() {
	caddy.RegisterPlugin("best", caddy.Plugin{
		ServerType: "dns",
		Action:     setup,
	})

}

func setup(c *caddy.Controller) error {
	version, authors, err := parse(c)
	if err != nil {
		return plugin.Error("best", err)
	}

	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		return Best{Next: next, Version: version, Authors: authors}
	})

	if len(privateIPBlocks) == 0 {
		for _, cidr := range []string{
			"127.0.0.0/8",    // IPv4 loopback
			"10.0.0.0/8",     // RFC1918
			"172.16.0.0/12",  // RFC1918
			"192.168.0.0/16", // RFC1918
			"::1/128",        // IPv6 loopback
			"fe80::/10",      // IPv6 link-local
			"fc00::/7",       // IPv6 unique local addr
		} {
			_, block, _ := net.ParseCIDR(cidr)
			privateIPBlocks = append(privateIPBlocks, block)
		}
	}
	return nil
}

func parse(c *caddy.Controller) (string, []string, error) {
	// Set here so we pick up AppName and AppVersion that get set in coremain's init().
	chaosVersion = caddy.AppName + "-" + caddy.AppVersion
	version := ""

	for c.Next() {
		args := c.RemainingArgs()
		if len(args) == 0 {
			return trim(chaosVersion), Owners, nil
		}
		if len(args) == 1 {
			return trim(args[0]), Owners, nil
		}

		version = args[0]
		authors := make(map[string]struct{})
		for _, a := range args[1:] {
			authors[a] = struct{}{}
		}
		list := []string{}
		for k := range authors {
			k = trim(k) // limit size to 255 chars
			list = append(list, k)
		}
		sort.Strings(list)
		return version, list, nil
	}

	return version, Owners, nil
}

func trim(s string) string {
	if len(s) < 256 {
		return s
	}
	return s[:255]
}

var chaosVersion string
var privateIPBlocks []*net.IPNet


func isPrivateIP(ip net.IP) bool {
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}
	return false
}