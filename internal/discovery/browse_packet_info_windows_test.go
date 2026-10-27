package discovery

import (
	"encoding/binary"
	"net"
	"strconv"
	"testing"
	"time"

	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
	"golang.org/x/sys/windows"
)

func windowsPacketInfoForTest(level, kind int, dst net.IP, index uint32) []byte {
	const wordSize = strconv.IntSize / 8
	const headerSize = wordSize + 8
	length := headerSize + len(dst) + 4
	oob := make([]byte, (length+wordSize-1)&^(wordSize-1))
	if wordSize == 8 {
		binary.NativeEndian.PutUint64(oob, uint64(length))
	} else {
		binary.NativeEndian.PutUint32(oob, uint32(length))
	}
	binary.NativeEndian.PutUint32(oob[wordSize:], uint32(level))
	binary.NativeEndian.PutUint32(oob[wordSize+4:], uint32(kind))
	copy(oob[headerSize:], dst)
	binary.NativeEndian.PutUint32(oob[headerSize+len(dst):], index)
	return oob
}

func TestWindowsBrowsePacketInfo(t *testing.T) {
	for _, tc := range []struct {
		name        string
		level, kind int
		dst         net.IP
	}{
		{"IPv4", windows.IPPROTO_IP, windows.IP_PKTINFO, net.IPv4(224, 0, 0, 251).To4()},
		{"IPv6", windows.IPPROTO_IPV6, windows.IPV6_PKTINFO, net.ParseIP("ff02::fb")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			oob := windowsPacketInfoForTest(tc.level, tc.kind, tc.dst, 7)
			index, dst, errInfo := parseWindowsBrowsePacketInfo(oob, tc.level, tc.kind)
			if errInfo != nil || index != 7 || !dst.Equal(tc.dst) {
				t.Fatalf("packet information = %d, %v, %v", index, dst, errInfo)
			}
			for name, invalid := range map[string][]byte{
				"missing":           nil,
				"truncated header":  oob[:3],
				"truncated payload": oob[:len(oob)-5],
				"zero length":       make([]byte, len(oob)),
				"missing interface": windowsPacketInfoForTest(tc.level, tc.kind, tc.dst, 0),
				"different message": windowsPacketInfoForTest(tc.level, tc.kind+1, tc.dst, 7),
			} {
				t.Run(name, func(t *testing.T) {
					if _, _, errInvalid := parseWindowsBrowsePacketInfo(invalid, tc.level, tc.kind); errInvalid == nil {
						t.Fatal("invalid packet information accepted")
					}
				})
			}
		})
	}
}

func TestWindowsBrowseReceivesPacketInfo(t *testing.T) {
	for _, network := range []string{"udp4", "udp6"} {
		t.Run(network, func(t *testing.T) {
			ip := net.IPv4(127, 0, 0, 1)
			if network == "udp6" {
				ip = net.IPv6loopback
			}
			conn, errListen := net.ListenUDP(network, &net.UDPAddr{IP: ip})
			if errListen != nil {
				t.Fatal(errListen)
			}
			t.Cleanup(func() {
				if errClose := conn.Close(); errClose != nil {
					t.Errorf("close packet information socket: %v", errClose)
				}
			})
			var read func() (int, net.IP, error)
			if network == "udp4" {
				packet := ipv4.NewPacketConn(conn)
				if errEnable := enableBrowsePacketInfo4(packet); errEnable != nil {
					t.Fatal(errEnable)
				}
				read = func() (int, net.IP, error) {
					_, cm, _, errRead := readBrowsePacket4(packet, make([]byte, 32))
					return cm.IfIndex, cm.Dst, errRead
				}
			} else {
				packet := ipv6.NewPacketConn(conn)
				if errEnable := enableBrowsePacketInfo6(packet); errEnable != nil {
					t.Fatal(errEnable)
				}
				read = func() (int, net.IP, error) {
					_, cm, _, errRead := readBrowsePacket6(packet, make([]byte, 32))
					return cm.IfIndex, cm.Dst, errRead
				}
			}
			type packetInfo struct {
				index int
				dst   net.IP
				err   error
			}
			done := make(chan packetInfo, 1)
			go func() {
				index, dst, errRead := read()
				done <- packetInfo{index: index, dst: dst, err: errRead}
			}()
			if _, errWrite := conn.WriteToUDP([]byte("metadata"), conn.LocalAddr().(*net.UDPAddr)); errWrite != nil {
				t.Fatal(errWrite)
			}
			select {
			case info := <-done:
				if info.err != nil || info.index == 0 || !info.dst.Equal(ip) {
					t.Fatalf("received packet information = %d, %v, %v", info.index, info.dst, info.err)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("packet information receive did not complete")
			}
		})
	}
}
