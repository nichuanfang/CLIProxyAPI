package discovery

import (
	"encoding/binary"
	"fmt"
	"net"
	"strconv"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/windows"
)

// x/net does not implement receive control messages on Windows. ReadMsgUDP
// uses Winsock WSARecvMsg and preserves the same receive isolation metadata.
func enableWindowsBrowsePacketInfo(conn net.PacketConn, level, option int) error {
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		return fmt.Errorf("discovery: packet information requires a UDP connection")
	}
	raw, errRaw := udp.SyscallConn()
	if errRaw != nil {
		return errRaw
	}
	var errOption error
	if errControl := raw.Control(func(fd uintptr) {
		errOption = windows.SetsockoptInt(windows.Handle(fd), level, option, 1)
	}); errControl != nil {
		return errControl
	}
	return errOption
}

func enableBrowsePacketInfo4(conn *ipv4.PacketConn) error {
	return enableWindowsBrowsePacketInfo(conn.PacketConn, windows.IPPROTO_IP, windows.IP_PKTINFO)
}

func enableBrowsePacketInfo6(conn *ipv6.PacketConn) error {
	return enableWindowsBrowsePacketInfo(conn.PacketConn, windows.IPPROTO_IPV6, windows.IPV6_PKTINFO)
}

func parseWindowsBrowsePacketInfo(oob []byte, level, kind int) (int, net.IP, error) {
	const wordSize = strconv.IntSize / 8
	const headerSize = wordSize + 8 // WSACMSGHDR: SIZE_T length, INT level, INT type.
	for len(oob) >= headerSize {
		length := uint64(binary.NativeEndian.Uint32(oob))
		if wordSize == 8 {
			length = binary.NativeEndian.Uint64(oob)
		}
		if length < headerSize || length > uint64(len(oob)) {
			return 0, nil, fmt.Errorf("discovery: invalid Windows control message length")
		}
		if int(binary.NativeEndian.Uint32(oob[wordSize:])) == level && int(binary.NativeEndian.Uint32(oob[wordSize+4:])) == kind {
			ipLen := net.IPv4len
			if level == windows.IPPROTO_IPV6 {
				ipLen = net.IPv6len
			}
			data := oob[headerSize:int(length)]
			if len(data) < ipLen+4 {
				return 0, nil, fmt.Errorf("discovery: truncated Windows packet information")
			}
			index := int(binary.NativeEndian.Uint32(data[ipLen:]))
			if index == 0 {
				return 0, nil, fmt.Errorf("discovery: missing Windows receive interface")
			}
			return index, append(net.IP(nil), data[:ipLen]...), nil
		}
		aligned := (int(length) + wordSize - 1) &^ (wordSize - 1)
		if aligned > len(oob) {
			break
		}
		oob = oob[aligned:]
	}
	return 0, nil, fmt.Errorf("discovery: missing Windows packet information")
}

func readWindowsBrowsePacket(conn net.PacketConn, buf []byte, level, kind int) (int, int, net.IP, net.Addr, error) {
	udp, ok := conn.(*net.UDPConn)
	if !ok {
		return 0, 0, nil, nil, fmt.Errorf("discovery: packet information requires a UDP connection")
	}
	var oob [128]byte
	n, oobn, flags, src, errRead := udp.ReadMsgUDP(buf, oob[:])
	if errRead != nil {
		return 0, 0, nil, nil, errRead
	}
	if flags&(windows.MSG_CTRUNC|windows.MSG_TRUNC) != 0 {
		return 0, 0, nil, nil, fmt.Errorf("discovery: truncated Windows browse packet")
	}
	index, dst, errInfo := parseWindowsBrowsePacketInfo(oob[:oobn], level, kind)
	return n, index, dst, src, errInfo
}

func readBrowsePacket4(conn *ipv4.PacketConn, buf []byte) (int, *ipv4.ControlMessage, net.Addr, error) {
	n, index, dst, src, errRead := readWindowsBrowsePacket(conn.PacketConn, buf, windows.IPPROTO_IP, windows.IP_PKTINFO)
	return n, &ipv4.ControlMessage{IfIndex: index, Dst: dst}, src, errRead
}

func readBrowsePacket6(conn *ipv6.PacketConn, buf []byte) (int, *ipv6.ControlMessage, net.Addr, error) {
	n, index, dst, src, errRead := readWindowsBrowsePacket(conn.PacketConn, buf, windows.IPPROTO_IPV6, windows.IPV6_PKTINFO)
	return n, &ipv6.ControlMessage{IfIndex: index, Dst: dst}, src, errRead
}
