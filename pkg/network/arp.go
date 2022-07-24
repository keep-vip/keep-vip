//go:build linux
// +build linux

package network

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"github.com/pkg/errors"
	"keep-vip/pkg/zlog"
	"net"
	"syscall"
	"unsafe"
)

const (
	opARPRequest = 1
	opARPReply   = 2
	hwLen        = 6
)

var (
	ethernetBroadcast = net.HardwareAddr{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
)

// arpHeader specifies the header for an ARP message.
type arpHeader struct {
	hardwareType          uint16
	protocolType          uint16
	hardwareAddressLength uint8
	protocolAddressLength uint8
	opcode                uint16
}

// arpMessage represents an ARP message.
type arpMessage struct {
	arpHeader
	senderHardwareAddress []byte
	senderProtocolAddress []byte
	targetHardwareAddress []byte
	targetProtocolAddress []byte
}

//bytes
func (m *arpMessage) bytes() ([]byte, error) {
	buf := new(bytes.Buffer)

	if err := binary.Write(buf, binary.BigEndian, m.arpHeader); err != nil {
		return nil, errors.Wrap(err, "binary write failed")
	}
	buf.Write(m.senderHardwareAddress)
	buf.Write(m.senderProtocolAddress)
	buf.Write(m.targetHardwareAddress)
	buf.Write(m.targetProtocolAddress)

	return buf.Bytes(), nil
}

// ARPSendGratuitous 通过指定网卡发送Gratuitous ARP消息
func ARPSendGratuitous(address, ifaceName string) error {
	iface, err := net.InterfaceByName(ifaceName)
	if err != nil {
		return errors.Wrapf(err, "failed to get interface %s", ifaceName)
	}
	// IP address
	ip := net.ParseIP(address)
	if ip.To4() == nil {
		return errors.New(address + ": is not an IPv4 address")
	}
	// MAC address
	if len(iface.HardwareAddr) != hwLen {
		return errors.New(iface.HardwareAddr.String() + ": is not an Ethernet MAC address")
	}
	zlog.Debug(fmt.Sprintf("Broadcasting ARP update for %s (%s) via %s", address, iface.HardwareAddr, iface.Name))
	m := &arpMessage{
		arpHeader{
			1,           // Ethernet
			0x0800,      // IPv4
			hwLen,       // 48-bit MAC Address
			net.IPv4len, // 32-bit IPv4 Address
			opARPReply,  // ARP Reply
		},
		iface.HardwareAddr,
		ip.To4(),
		ethernetBroadcast,
		net.IPv4bcast,
	}

	return sendARP(iface, m)
}

func sendARP(iface *net.Interface, m *arpMessage) error {
	fd, err := syscall.Socket(syscall.AF_PACKET, syscall.SOCK_DGRAM, int(htons(syscall.ETH_P_ARP)))
	if err != nil {
		return errors.WithStack(err)
	}
	defer func(fd int) {
		if err := syscall.Close(fd); err != nil {
			zlog.Warn(err.Error())
		}
	}(fd)
	if err := syscall.BindToDevice(fd, iface.Name); err != nil {
		return errors.Wrap(err, "failed to bind to device")
	}
	ll := syscall.SockaddrLinklayer{
		Protocol: htons(syscall.ETH_P_ARP),
		Ifindex:  iface.Index,
		Pkttype:  0, // syscall.PACKET_HOST
		Hatype:   m.hardwareType,
		Halen:    m.hardwareAddressLength,
	}

	target := ethernetBroadcast
	if m.opcode == opARPReply {
		target = m.targetHardwareAddress
	}
	for i := 0; i < len(target); i++ {
		ll.Addr[i] = target[i]
	}
	b, err := m.bytes()
	if err != nil {
		return errors.Wrap(err, "failed to convert ARP message")
	}

	if err := syscall.Bind(fd, &ll); err != nil {
		return errors.Wrap(err, "failed to bind")
	}
	if err := syscall.Sendto(fd, b, 0, &ll); err != nil {
		return errors.Wrap(err, "failed to send")
	}
	return nil
}

func htons(p uint16) uint16 {
	var b [2]byte
	binary.BigEndian.PutUint16(b[:], p)
	return *(*uint16)(unsafe.Pointer(&b))
}
