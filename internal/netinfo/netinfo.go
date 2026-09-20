// Package netinfo discovers the local network interfaces so the server can tell
// the user which URLs may be useful from another device on the same network.
//
// The logic is split into small pure functions so it can be tested without
// depending on the real network interfaces of the machine running the tests.
package netinfo

import (
	"net"
	"sort"
	"strings"
)

// Address describes a single usable IPv4 address of a network interface.
type Address struct {
	// Interface is the operating system name of the interface, for example
	// "Wi-Fi" or "eth0".
	Interface string
	// IP is the IPv4 address.
	IP net.IP
	// Loopback reports whether the address is the loopback address.
	Loopback bool
	// Virtual reports whether the interface looks like a virtual adapter.
	Virtual bool
	// Up reports whether the interface is administratively up.
	Up bool
}

// URL renders the address as a browsable URL for the given port.
func (a Address) URL(port string) string {
	return "http://" + a.IP.String() + ":" + port
}

// Collect enumerates the network interfaces and returns their IPv4 addresses,
// including loopback. It never fails: errors on individual interfaces are
// skipped so a partially readable system still produces useful output.
func Collect() []Address {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}

	addrs := make([]Address, 0, len(ifaces))
	for _, iface := range ifaces {
		addrs = append(addrs, collectInterface(iface)...)
	}

	return Sort(addrs)
}

func collectInterface(iface net.Interface) []Address {
	// Down or not-yet-configured interfaces cannot accept traffic.
	if iface.Flags&net.FlagUp == 0 {
		return nil
	}

	// Interfaces without an address (for example an idle bridge) are noise.
	ifaceAddrs, err := iface.Addrs()
	if err != nil {
		return nil
	}

	var out []Address
	for _, addr := range ifaceAddrs {
		ip := ipv4Of(addr)
		if ip == nil {
			continue
		}

		out = append(out, Address{
			Interface: iface.Name,
			IP:        ip,
			Loopback:  ip.IsLoopback(),
			Virtual:   IsVirtualInterface(iface.Name),
			Up:        true,
		})
	}

	return out
}

// ipv4Of extracts an IPv4 address from a net.Addr, returning nil for IPv6 or
// unparsable entries.
func ipv4Of(addr net.Addr) net.IP {
	ipnet, ok := addr.(*net.IPNet)
	if !ok {
		return nil
	}

	ip := ipnet.IP.To4()
	if ip == nil {
		return nil
	}

	return ip
}

// virtualKeywords are name fragments commonly used by virtual adapters. The
// list is only used as a hint for the printed output; it is never used to
// decide correctness.
var virtualKeywords = []string{
	"vmware",
	"virtualbox",
	"vbox",
	"hyper-v",
	"vethernet",
	"wsl",
	"docker",
	"br-",
	"veth",
	"virbr",
	"tailscale",
	"zerotier",
	"npcap",
	"loopback",
	"bluetooth",
	"teredo",
	"isatap",
}

// IsVirtualInterface reports whether an interface name looks like a virtual
// adapter (VPN, container, virtual machine or tunnel).
func IsVirtualInterface(name string) bool {
	lower := strings.ToLower(name)
	for _, kw := range virtualKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// Sort orders addresses for display: loopback last, then physical interfaces
// before virtual ones, then by interface name and address.
func Sort(addrs []Address) []Address {
	sorted := make([]Address, len(addrs))
	copy(sorted, addrs)

	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]

		if a.Loopback != b.Loopback {
			return !a.Loopback
		}
		if a.Virtual != b.Virtual {
			return !a.Virtual
		}
		if a.Interface != b.Interface {
			return a.Interface < b.Interface
		}

		return a.IP.String() < b.IP.String()
	})

	return sorted
}
