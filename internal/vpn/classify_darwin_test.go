//go:build darwin

package vpn

import (
	"net"
	"testing"
)

// The rule is about what the device is, not what it is called: a tunnel
// carries IP with no link layer of its own, so it has no MAC address and
// is point-to-point. Wi-Fi and Ethernet always have a MAC.
func TestIsTunnelOnDarwin(t *testing.T) {
	mac := net.HardwareAddr{0xde, 0xad, 0xbe, 0xef, 0x00, 0x01}

	cases := []struct {
		name  string
		iface net.Interface
		want  bool
	}{
		{
			"a NetworkExtension VPN (WireGuard, OpenVPN, IKEv2, Tailscale)",
			net.Interface{Name: "utun4", Flags: net.FlagUp | net.FlagPointToPoint | net.FlagMulticast},
			true,
		},
		{
			"a ppp link",
			net.Interface{Name: "ppp0", Flags: net.FlagUp | net.FlagPointToPoint},
			true,
		},
		{
			"wi-fi",
			net.Interface{Name: "en0", HardwareAddr: mac, Flags: net.FlagUp | net.FlagBroadcast | net.FlagMulticast},
			false,
		},
		{
			"a bridge",
			net.Interface{Name: "bridge0", HardwareAddr: mac, Flags: net.FlagUp | net.FlagBroadcast},
			false,
		},
		{
			// The name says tunnel and it is point-to-point, but it has a
			// link layer of its own, so it is not carrying IP anywhere.
			"a physical device that merely sounds like one",
			net.Interface{Name: "tun9", HardwareAddr: mac, Flags: net.FlagUp | net.FlagPointToPoint},
			false,
		},
	}

	for _, c := range cases {
		got, why, err := isTunnel(c.iface)
		if err != nil {
			t.Errorf("%s: isTunnel failed: %v", c.name, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s (%s): isTunnel = %v, want %v", c.name, c.iface.Name, got, c.want)
		}
		if !got && why == "" {
			t.Errorf("%s: isTunnel said no without saying why", c.name)
		}
	}
}
