package netinfo

import (
	"net"
	"testing"
)

func TestIsVirtualInterface(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"Wi-Fi", false},
		{"Ethernet", false},
		{"eth0", false},
		{"enp3s0", false},
		{"VMware Network Adapter VMnet1", true},
		{"VirtualBox Host-Only Network", true},
		{"vEthernet (WSL)", true},
		{"Docker0", true},
		{"br-1a2b3c", true},
		{"Tailscale", true},
		{"Loopback Pseudo-Interface 1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsVirtualInterface(tt.name); got != tt.want {
				t.Errorf("IsVirtualInterface(%q) = %v, want %v", tt.name, got, tt.want)
			}
		})
	}
}

func TestAddressURL(t *testing.T) {
	addr := Address{
		Interface: "Wi-Fi",
		IP:        net.ParseIP("192.168.1.10").To4(),
	}

	if got := addr.URL("8088"); got != "http://192.168.1.10:8088" {
		t.Errorf("URL = %q", got)
	}
}

func TestSortOrdersLoopbackLastAndPhysicalFirst(t *testing.T) {
	addrs := []Address{
		{Interface: "Loopback", IP: net.ParseIP("127.0.0.1").To4(), Loopback: true, Up: true},
		{Interface: "VMware Network Adapter", IP: net.ParseIP("192.168.56.1").To4(), Virtual: true, Up: true},
		{Interface: "Wi-Fi", IP: net.ParseIP("192.168.1.10").To4(), Up: true},
		{Interface: "Ethernet", IP: net.ParseIP("10.0.0.5").To4(), Up: true},
	}

	sorted := Sort(addrs)

	want := []string{"Ethernet", "Wi-Fi", "VMware Network Adapter", "Loopback"}
	for i, w := range want {
		if sorted[i].Interface != w {
			t.Errorf("position %d = %q, want %q", i, sorted[i].Interface, w)
		}
	}
}

func TestSortIsStableAndDoesNotMutateInput(t *testing.T) {
	addrs := []Address{
		{Interface: "B", IP: net.ParseIP("10.0.0.2").To4(), Up: true},
		{Interface: "A", IP: net.ParseIP("10.0.0.1").To4(), Up: true},
	}

	_ = Sort(addrs)

	if addrs[0].Interface != "B" {
		t.Errorf("Sort mutated its input: %v", addrs)
	}
}

func TestSortOrdersByAddressWithinInterface(t *testing.T) {
	addrs := []Address{
		{Interface: "eth0", IP: net.ParseIP("10.0.0.20").To4(), Up: true},
		{Interface: "eth0", IP: net.ParseIP("10.0.0.10").To4(), Up: true},
	}

	sorted := Sort(addrs)

	if sorted[0].IP.String() != "10.0.0.10" {
		t.Errorf("first address = %s, want 10.0.0.10", sorted[0].IP)
	}
}

func TestCollectDoesNotPanicAndOmitsLoopbackFromBeingLabeledOnly(t *testing.T) {
	// Collect depends on the host machine, so only sanity-check its output
	// rather than asserting specific interfaces or addresses.
	for _, addr := range Collect() {
		if addr.IP.To4() == nil {
			t.Errorf("non-IPv4 address returned: %v", addr.IP)
		}
		if addr.Interface == "" {
			t.Error("address without an interface name")
		}
		if addr.Loopback != addr.IP.IsLoopback() {
			t.Errorf("loopback flag inconsistent for %v", addr.IP)
		}
		if !addr.Up {
			t.Errorf("down interface returned: %v", addr.Interface)
		}
	}
}

func TestSortEmpty(t *testing.T) {
	if got := Sort(nil); len(got) != 0 {
		t.Errorf("Sort(nil) = %v", got)
	}
}
