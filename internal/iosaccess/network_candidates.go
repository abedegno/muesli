// Package iosaccess implements the local (Electron-managed) side of issue
// #767's native iOS access: eligible network candidate enumeration, the
// per-address ECDSA/self-signed certificate lifecycle, and QR pairing
// payload construction/validation. It has no dependency on Electron or the
// HTTP server — those consume this package's pure types.
package iosaccess

import (
	"net"
	"strings"
)

// Family is the IP address family of a network candidate.
type Family string

const (
	IPv4 Family = "ipv4"
	IPv6 Family = "ipv6"
)

// InterfaceAddressPair is one eligible (interface name, address, family)
// triple — the authoritative identity of a local iOS access candidate. Full-
// triple equality defines identity: the same address moving to a different
// interface, or a different address appearing on the same interface, is a
// distinct pair.
type InterfaceAddressPair struct {
	InterfaceName string
	Address       string
	Family        Family
}

// Equal reports whether p and o are the same candidate.
func (p InterfaceAddressPair) Equal(o InterfaceAddressPair) bool {
	return p.InterfaceName == o.InterfaceName && p.Address == o.Address && p.Family == o.Family
}

// RawInterface is one already-active, non-loopback network interface as
// reported by the OS, before eligibility filtering. Addresses may be plain
// IPs, CIDR notation ("192.168.1.20/24"), or carry an IPv6 zone id
// ("fe80::1%en0") — all forms the OS-native enumerators (Go's net.Interfaces,
// Node's os.networkInterfaces) can produce. Activity/loopback filtering is
// the caller's responsibility (see InterfaceAddressSource in control.go);
// this type and EligiblePairs operate purely on whatever list they're given.
type RawInterface struct {
	Name      string
	Addresses []string
}

// EligiblePairs derives every eligible InterfaceAddressPair from a raw
// interface snapshot, per the accepted spec for issue #767: RFC 1918 IPv4 or
// IPv6 ULA (fc00::/7) addresses are eligible; public, loopback, wildcard,
// IPv4 link-local (169.254.0.0/16), and IPv6 link-local (fe80::/10)
// addresses are excluded. One candidate is produced for every eligible
// interface-address pair — multiple eligible addresses on one interface,
// dual-stack interfaces, and the same address appearing on different
// interfaces each yield their own distinct pairs.
//
// The result order matches input order (interface order, then address order
// within each interface) so callers presenting candidates to a user get a
// stable, predictable list.
func EligiblePairs(interfaces []RawInterface) []InterfaceAddressPair {
	out := []InterfaceAddressPair{}
	for _, iface := range interfaces {
		for _, raw := range iface.Addresses {
			ip, ok := parseAddress(raw)
			if !ok {
				continue
			}
			family, eligible := classify(ip)
			if !eligible {
				continue
			}
			out = append(out, InterfaceAddressPair{
				InterfaceName: iface.Name,
				Address:       ip.String(),
				Family:        family,
			})
		}
	}
	return out
}

// parseAddress strips an optional IPv6 zone id and CIDR suffix and parses
// the remaining textual IP.
func parseAddress(raw string) (net.IP, bool) {
	if i := strings.IndexByte(raw, '%'); i >= 0 {
		raw = raw[:i]
	}
	if i := strings.IndexByte(raw, '/'); i >= 0 {
		raw = raw[:i]
	}
	ip := net.ParseIP(raw)
	return ip, ip != nil
}

// classify reports the address family and eligibility of ip per the
// accepted spec's inclusion/exclusion rules.
func classify(ip net.IP) (Family, bool) {
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return "", false
	}
	if v4 := ip.To4(); v4 != nil {
		if isRFC1918(v4) {
			return IPv4, true
		}
		return "", false
	}
	if isULA(ip) {
		return IPv6, true
	}
	return "", false
}

// isRFC1918 reports whether a 4-byte IPv4 address falls in 10.0.0.0/8,
// 172.16.0.0/12, or 192.168.0.0/16.
func isRFC1918(v4 net.IP) bool {
	switch {
	case v4[0] == 10:
		return true
	case v4[0] == 172 && v4[1] >= 16 && v4[1] <= 31:
		return true
	case v4[0] == 192 && v4[1] == 168:
		return true
	default:
		return false
	}
}

// isULA reports whether ip is an IPv6 Unique Local Address (fc00::/7).
func isULA(ip net.IP) bool {
	v6 := ip.To16()
	if v6 == nil || ip.To4() != nil {
		return false
	}
	return v6[0]&0xFE == 0xFC
}

// SelectionOutcome models what an "Allow iOS access" enablement attempt
// found, mirroring the accepted spec's exactly-one-of-three behavior:
// zero candidates fails visibly, exactly one auto-selects, and two or more
// require explicit user selection.
type SelectionOutcome struct {
	Candidates []InterfaceAddressPair
	// AutoSelected is non-nil only when exactly one candidate exists.
	AutoSelected *InterfaceAddressPair
}

// Evaluate classifies a candidate list into the zero/one/many outcome.
func Evaluate(candidates []InterfaceAddressPair) SelectionOutcome {
	out := SelectionOutcome{Candidates: candidates}
	if len(candidates) == 1 {
		c := candidates[0]
		out.AutoSelected = &c
	}
	return out
}
