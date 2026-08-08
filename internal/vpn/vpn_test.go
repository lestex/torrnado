package vpn

import (
	"net"
	"testing"
)

// The interface list is the escape hatch for tunnels the kernel cannot
// label, so it has to win before any classification happens.
func TestNamedInterfacesMatchExactly(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
		want  bool
	}{
		{"utun4", []string{"utun4"}, true},
		{"utun4", []string{"en0", "utun4"}, true},
		{"utun4", nil, false},
		{"utun4", []string{"utun"}, false},        // a prefix is not a match
		{"tunnelbroker0", []string{"tun"}, false}, // nor is it the other way round
	}
	for _, c := range cases {
		if got := named(c.name, c.extra); got != c.want {
			t.Errorf("named(%q, %v) = %v, want %v", c.name, c.extra, got, c.want)
		}
	}
}

// interfaceForIP is how a source address becomes a device, so it has to
// find an address the machine really has - and refuse one it does not.
func TestInterfaceForIPFindsTheOwningDevice(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skipf("no interfaces to test against: %v", err)
	}

	var (
		wantName string
		addr     net.IP
	)
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil || len(addrs) == 0 {
			continue
		}
		if ip := addrIP(addrs[0]); ip != nil {
			wantName, addr = iface.Name, ip
			break
		}
	}
	if addr == nil {
		t.Skip("no interface on this machine has an address")
	}

	got, err := interfaceForIP(addr)
	if err != nil {
		t.Fatalf("interfaceForIP(%s) failed: %v", addr, err)
	}
	if got.Name != wantName {
		t.Errorf("interfaceForIP(%s) = %q, want %q", addr, got.Name, wantName)
	}

	// An address nothing holds must be an error rather than whichever
	// interface happened to be first.
	if _, err := interfaceForIP(net.ParseIP("203.0.113.7")); err == nil {
		t.Error("an address held by no interface was matched to one anyway")
	}
}

// A guard that fails open is not a guard: every path that cannot answer
// has to say "not on a VPN", not "probably fine".
func TestDetectFailsClosed(t *testing.T) {
	st, err := Detect(nil)
	if err != nil {
		if st.Active {
			t.Errorf("Detect reported Active despite failing: %v", err)
		}
		if st.Reason == "" {
			t.Error("a failed Detect gave no reason")
		}
		return
	}

	// It succeeded, which means this machine has a route. Then the verdict
	// must be self-consistent: an interface was identified either way, and
	// anything not active explains itself.
	if st.Interface == "" {
		t.Error("Detect succeeded without naming an interface")
	}
	if !st.Active && st.Reason == "" {
		t.Errorf("Detect said no VPN on %s without saying why", st.Interface)
	}
	if st.Active && st.Reason != "" {
		t.Errorf("Detect said VPN on %s but also gave a reason not to: %q", st.Interface, st.Reason)
	}
}

func TestStatusStringSaysWhatHappened(t *testing.T) {
	cases := []struct {
		st   Status
		want string
	}{
		{Status{Active: true, Interface: "utun4"}, "on utun4"},
		{Status{Reason: "traffic leaves by en0, which is a physical device"},
			"traffic leaves by en0, which is a physical device"},
		{Status{}, "no VPN"},
	}
	for _, c := range cases {
		if got := c.st.String(); got != c.want {
			t.Errorf("Status%+v.String() = %q, want %q", c.st, got, c.want)
		}
	}
}
