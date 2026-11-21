// Package netguard rejects upstream addresses that are not public unicast addresses. It is used by
// the gateway dialer and by operator-triggered discovery so both follow the same rule.
package netguard

import (
	"context"
	"errors"
	"net"
	"net/netip"
)

// Restricted reports whether an address cannot be reached unless the channel allows private
// networks. IPv4-compatible IPv6 (::/96) is listed explicitly because netip does not decode it but
// a dialer may still route it to the embedded IPv4 address.
func Restricted(address netip.Addr) bool {
	if !address.IsValid() || !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() {
		return true
	}
	for _, prefix := range restrictedPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

// Resolve checks every address of a host and returns them, so a caller dials exactly the addresses
// that were checked instead of resolving a second time.
func Resolve(ctx context.Context, host string, allowPrivate bool) ([]netip.Addr, error) {
	addresses, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	if len(addresses) == 0 {
		return nil, errors.New("the host has no addresses")
	}
	if !allowPrivate {
		for _, address := range addresses {
			if Restricted(address.Unmap()) {
				return nil, errors.New("the host resolves to a restricted address")
			}
		}
	}
	return addresses, nil
}

var restrictedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("64:ff9b:1::/48"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
}
