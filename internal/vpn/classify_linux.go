//go:build linux

package vpn

import (
	"bufio"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// sysClassNet is where the kernel describes network devices. A variable so
// a test can point it at a tree it built itself.
var sysClassNet = "/sys/class/net"

// ARPHRD values from linux/if_arp.h. A tun device and a WireGuard device
// both report NONE -- they carry IP with no link layer underneath, which
// is what makes them tunnels.
const (
	arphrdPPP     = 512
	arphrdTunnel  = 768 // ipip
	arphrdTunnel6 = 769
	arphrdNone    = 65534
)

// tunnelDevTypes are the DEVTYPE values the kernel gives virtual devices
// that carry traffic somewhere else. Drivers that set one are easy;
// everything else falls through to the checks below.
var tunnelDevTypes = map[string]bool{
	"wireguard": true,
	"vti":       true,
	"vti6":      true,
	"xfrm":      true,
	"gre":       true,
	"ppp":       true,
}

// isTunnel reports whether iface is a tunnel device, by asking sysfs what
// kind of device it is rather than by matching its name.
//
// Names cannot be trusted here: wg-quick names a WireGuard interface after
// its config file, so a perfectly ordinary VPN can come up as "home" or
// "work", while a machine with no VPN at all can have something called
// tun0 for an unrelated reason.
//
// The second return value says why, for a Reason a user can act on.
func isTunnel(iface net.Interface) (bool, string, error) {
	dir := filepath.Join(sysClassNet, iface.Name)

	// The driver's own word for what it is, when it has one.
	devType, err := ueventValue(filepath.Join(dir, "uevent"), "DEVTYPE")
	if err != nil {
		return false, "", err
	}
	if tunnelDevTypes[devType] {
		return true, "", nil
	}

	// tun and tap devices have no DEVTYPE but do carry the flags the tun
	// driver was opened with. This is what OpenVPN and anything else built
	// on /dev/net/tun looks like.
	if _, err := os.Stat(filepath.Join(dir, "tun_flags")); err == nil {
		return true, "", nil
	}

	// Last, the link-layer type. A device carrying IP with no link layer
	// (NONE) or a point-to-point protocol is not something a packet
	// reaches the internet by directly.
	hwType, err := intFile(filepath.Join(dir, "type"))
	if err != nil {
		return false, "", err
	}
	switch hwType {
	case arphrdNone, arphrdPPP, arphrdTunnel, arphrdTunnel6:
		return true, "", nil
	}

	return false, "which is a physical device", nil
}

// ueventValue reads one KEY=value line out of a sysfs uevent file,
// returning "" when the key is absent -- which is normal, not an error.
func ueventValue(path, key string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	prefix := key + "="
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, prefix) {
			return strings.TrimPrefix(line, prefix), nil
		}
	}
	return "", sc.Err()
}

// intFile reads a sysfs file holding a single number.
func intFile(path string) (int, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(string(b)))
}
