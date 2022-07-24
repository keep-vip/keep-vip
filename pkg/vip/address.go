package vip

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"net"
	"sync"
)

// Network is an interface that enable managing operations for a given IP
type Network interface {
	AddIP() error
	DeleteIP() error
	IsSet() (bool, error)
	IP() string
	SetIP(ip string) error
	Interface() string
}

type network struct {
	mu sync.Mutex

	address *netlink.Addr
	link    netlink.Link

	dnsName string
	isDDNS  bool
}

func (n *network) AddIP() error {
	//TODO implement me
	panic("implement me")
}

func (n *network) DeleteIP() error {
	//TODO implement me
	panic("implement me")
}

func (n *network) IsSet() (bool, error) {
	//TODO implement me
	panic("implement me")
}

func (n *network) IP() string {
	//TODO implement me
	panic("implement me")
}

func (n *network) SetIP(ip string) error {
	//TODO implement me
	panic("implement me")
}

func (n *network) Interface() string {
	//TODO implement me
	panic("implement me")
}

// NewConfig - 网络配置接口
func NewConfig(vip, ifName string) (Network, error) {
	// 解析vip
	if ip := net.ParseIP(vip); ip.To4() == nil {
		return nil, errors.New(fmt.Sprintf("could not parse vip '%s'", vip))
	}
	address, err := netlink.ParseAddr(vip + "/32")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if ifName == "lo" {
		address.Scope = unix.RT_SCOPE_HOST
	}

	// 连接网络接口
	link, err := netlink.LinkByName(ifName)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &network{
		address: address,
		link:    link,
	}, nil
}
