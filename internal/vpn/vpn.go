// Package vpn reports whether the system's traffic leaves through a VPN.
//
// It asks the kernel two questions - which interface would carry a packet
// to the internet, and what kind of device is that - rather than knowing
// anything about any particular VPN client. WireGuard, OpenVPN, IKEv2,
// IPsec, PPP and macOS's NetworkExtension clients all end up as a tunnel
// device carrying the default route, so all of them are detected the same
// way, and a client this package has never heard of works too.
//
// It deliberately does not look at whether a tunnel device merely exists.
// A Mac typically has several utun interfaces up at all times - iCloud
// Private Relay, Handoff, a Tailscale that is connected but has no exit
// node - none of which carry general traffic. Only the interface that
// would actually carry the packet counts.
package vpn

import (
	"fmt"
	"net"
	"slices"
)

// Status is what the system says about the way out to the internet.
type Status struct {
	// Active reports that traffic leaves through a tunnel device.
	Active bool
	// Interface is what traffic would leave by ("utun4", "en0"), empty if
	// that could not be determined.
	Interface string
	// IP is the source address the kernel would choose.
	IP net.IP
	// Reason says why Active is false, phrased for a log line or a status
	// bar. Empty when Active is true.
	Reason string
}

// String renders a Status the way a log line or a status bar wants it.
func (s Status) String() string {
	if s.Active {
		return "on " + s.Interface
	}
	if s.Reason != "" {
		return s.Reason
	}
	return "no VPN"
}

// probeAddrs are dialled to find out which interface carries traffic to
// the internet. Both are documentation ranges (RFC 5737, RFC 3849): a UDP
// dial performs the route lookup and binds a source address without
// sending a packet, so nothing is ever contacted, but if the constants
// leak into some future code that does send, they go nowhere real.
//
// IPv6 is tried when IPv4 finds no route, so a v6-only host is not
// reported as having no way out at all.
var probeAddrs = []struct{ network, addr string }{
	{"udp4", "192.0.2.1:9"},
	{"udp6", "[2001:db8::1]:9"},
}

// Detect reports whether traffic to the internet leaves through a VPN.
//
// extra names interfaces to treat as a VPN whatever the kernel says about
// them, for setups the kernel cannot label: a policy-based IPsec tunnel
// moves traffic over the physical interface with no tunnel device at all,
// and nothing here can tell that apart from no VPN.
//
// It fails closed. Every error path returns a Status with Active false and
// a Reason, alongside the error, because a guard that fails open on a
// broken lookup is not a guard. Callers that only care about the verdict
// can ignore the error and read the Status.
func Detect(extra []string) (Status, error) {
	ip, err := outboundIP()
	if err != nil {
		return Status{Reason: "no route to the internet"}, err
	}

	iface, err := interfaceForIP(ip)
	if err != nil {
		return Status{IP: ip, Reason: fmt.Sprintf("no interface holds %s", ip)}, err
	}

	st := Status{Interface: iface.Name, IP: ip}

	if named(iface.Name, extra) {
		st.Active = true
		return st, nil
	}

	tunnel, why, err := isTunnel(iface)
	if err != nil {
		st.Reason = fmt.Sprintf("cannot tell what %s is: %v", iface.Name, err)
		return st, err
	}
	if !tunnel {
		st.Reason = fmt.Sprintf("traffic leaves by %s, %s", iface.Name, why)
		return st, nil
	}

	st.Active = true
	return st, nil
}

// named reports whether name was listed by the caller as a VPN interface.
// An exact match, not a prefix: a list is written by someone who knows
// which interface they mean, and "tun" quietly matching "tunnelbroker0"
// is not a favour.
func named(name string, extra []string) bool {
	return slices.Contains(extra, name)
}

// outboundIP is the source address the kernel would use for a packet to
// the internet.
//
// A "connected" UDP socket is not a connection: net.Dial on udp performs
// the route lookup and binds the local address the route implies, without
// putting anything on the wire. That makes this both free and silent, and
// it answers the question the route table would answer.
func outboundIP() (net.IP, error) {
	var lastErr error
	for _, p := range probeAddrs {
		c, err := net.Dial(p.network, p.addr)
		if err != nil {
			lastErr = err
			continue
		}
		addr, ok := c.LocalAddr().(*net.UDPAddr)
		c.Close()
		if !ok || addr.IP == nil || addr.IP.IsUnspecified() {
			lastErr = fmt.Errorf("no local address for a %s route", p.network)
			continue
		}
		return addr.IP, nil
	}
	return nil, fmt.Errorf("finding the outbound address: %w", lastErr)
}

// interfaceForIP finds the interface holding ip.
func interfaceForIP(ip net.IP) (net.Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return net.Interface{}, fmt.Errorf("listing interfaces: %w", err)
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			// One interface refusing to describe itself is not a reason to
			// give up on the rest.
			continue
		}
		for _, a := range addrs {
			if addrIP(a).Equal(ip) {
				return iface, nil
			}
		}
	}
	return net.Interface{}, fmt.Errorf("no interface has address %s", ip)
}

// addrIP pulls the address out of the two shapes Interface.Addrs returns.
func addrIP(a net.Addr) net.IP {
	switch v := a.(type) {
	case *net.IPNet:
		return v.IP
	case *net.IPAddr:
		return v.IP
	}
	return nil
}
