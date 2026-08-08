//go:build linux

package vpn

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeDevice writes the sysfs a kernel would write for one interface:
// uevent is always there, tunFlags and hwType only when the real device
// would have them.
func fakeDevice(t *testing.T, root, name, devType string, tunFlags bool, hwType string) {
	t.Helper()

	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	uevent := "INTERFACE=" + name + "\nIFINDEX=3\n"
	if devType != "" {
		uevent = "DEVTYPE=" + devType + "\n" + uevent
	}
	write := func(file, content string) {
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("uevent", uevent)
	if tunFlags {
		write("tun_flags", "0x1002\n")
	}
	if hwType != "" {
		write("type", hwType)
	}
}

// Names cannot be trusted on Linux - wg-quick names an interface after
// its config file, so a VPN routinely comes up as "home" - which is why
// this classifies by what sysfs says the device is.
func TestIsTunnelOnLinux(t *testing.T) {
	root := t.TempDir()
	sysClassNet = root
	t.Cleanup(func() { sysClassNet = "/sys/class/net" })

	// A WireGuard interface named after nothing in particular.
	fakeDevice(t, root, "home", "wireguard", false, "65534\n")
	// OpenVPN: no DEVTYPE, but the tun driver's flags are there.
	fakeDevice(t, root, "tun0", "", true, "65534\n")
	// A tap device, which is ARPHRD_ETHER and would otherwise look
	// physical - tun_flags is the only thing that gives it away.
	fakeDevice(t, root, "tap0", "", true, "1\n")
	// A dial-up-style link: no DEVTYPE, no tun_flags, ARPHRD_PPP.
	fakeDevice(t, root, "ppp0", "", false, "512\n")
	// Ordinary ethernet.
	fakeDevice(t, root, "eth0", "", false, "1\n")
	// A bridge: virtual, but traffic still leaves by a real link.
	fakeDevice(t, root, "br0", "bridge", false, "1\n")

	cases := []struct {
		name string
		want bool
	}{
		{"home", true},
		{"tun0", true},
		{"tap0", true},
		{"ppp0", true},
		{"eth0", false},
		{"br0", false},
	}
	for _, c := range cases {
		got, why, err := isTunnel(net.Interface{Name: c.name})
		if err != nil {
			t.Errorf("%s: isTunnel failed: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("isTunnel(%s) = %v, want %v", c.name, got, c.want)
		}
		if !got && why == "" {
			t.Errorf("isTunnel(%s) said no without saying why", c.name)
		}
	}
}

// An interface sysfs has nothing to say about must be an error, so Detect
// fails closed rather than treating "cannot tell" as "not a VPN, carry
// on" - the two look the same to a caller reading only the bool.
func TestIsTunnelErrorsOnAnUnknownDevice(t *testing.T) {
	sysClassNet = t.TempDir()
	t.Cleanup(func() { sysClassNet = "/sys/class/net" })

	if _, _, err := isTunnel(net.Interface{Name: "ghost0"}); err == nil {
		t.Error("a device with no sysfs entry was classified without complaint")
	}
}

func TestUeventValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uevent")
	if err := os.WriteFile(path, []byte("DEVTYPE=wireguard\nINTERFACE=wg0\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if got, err := ueventValue(path, "DEVTYPE"); err != nil || got != "wireguard" {
		t.Errorf("ueventValue(DEVTYPE) = %q, %v; want \"wireguard\", nil", got, err)
	}
	// A missing key is the normal case for most drivers, not a failure.
	if got, err := ueventValue(path, "NOPE"); err != nil || got != "" {
		t.Errorf("ueventValue(NOPE) = %q, %v; want \"\", nil", got, err)
	}
}
