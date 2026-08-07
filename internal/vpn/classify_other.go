//go:build !linux && !darwin

package vpn

import (
	"fmt"
	"net"
)

// isTunnel has nothing to work with on a platform whose device model this
// package does not know, so it says so rather than guessing from the name.
//
// This exists so the package -- and the CLI that imports it -- still
// builds where the daemon does not run. Detect's own interface list is the
// way to make the guard usable here: name the interface and it counts.
func isTunnel(iface net.Interface) (bool, string, error) {
	return false, "", fmt.Errorf("classifying %s: not supported on this platform; list it in vpn.interfaces instead", iface.Name)
}
