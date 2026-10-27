//go:build !windows

package discovery

import (
	"net"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

func enableBrowsePacketInfo4(conn *ipv4.PacketConn) error {
	return conn.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true)
}

func enableBrowsePacketInfo6(conn *ipv6.PacketConn) error {
	return conn.SetControlMessage(ipv6.FlagInterface|ipv6.FlagDst, true)
}

func readBrowsePacket4(conn *ipv4.PacketConn, buf []byte) (int, *ipv4.ControlMessage, net.Addr, error) {
	return conn.ReadFrom(buf)
}

func readBrowsePacket6(conn *ipv6.PacketConn, buf []byte) (int, *ipv6.ControlMessage, net.Addr, error) {
	return conn.ReadFrom(buf)
}
