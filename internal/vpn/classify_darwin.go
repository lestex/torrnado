//go:build darwin

package vpn

import "net"

// isTunnel reports whether iface is a tunnel device, by what the device
// is rather than what it is called.
//
// macOS has no /sys to ask, but it does not need one: every VPN on this
// platform terminates on a point-to-point device with no link layer of its
// own -- utun for anything built on NetworkExtension (WireGuard, OpenVPN,
// IKEv2, the built-in IPsec client, Tailscale, every commercial client),
// ppp for dial-up-style links, ipsec for the legacy racoon devices.
// Ethernet and Wi-Fi are broadcast devices with a MAC address and can
// never match, which is the property being leaned on.
//
// The second return value says why, for a Reason a user can act on.
func isTunnel(iface net.Interface) (bool, string, error) {
	if len(iface.HardwareAddr) != 0 {
		return false, "which is a physical device", nil
	}
	if iface.Flags&net.FlagPointToPoint == 0 {
		return false, "which is not a point-to-point device", nil
	}
	return true, "", nil
}
