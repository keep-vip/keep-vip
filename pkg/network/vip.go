package network

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"keep-vip/pkg/zlog"
	"net"
)

type Vip interface {
	IsExist() (bool, error)
	AddVIP() error
	DeleteVIP() error
	String() string
	Interface() string
}

type VipInterface struct {
	address *netlink.Addr
	link    netlink.Link
}

// IsExist - 检查VIP是否存在
func (v *VipInterface) IsExist() (bool, error) {
	if v.address == nil {
		return false, nil
	}
	// 获取网卡的IP地址列表
	adders, err := netlink.AddrList(v.link, 0)
	if err != nil {
		return false, errors.WithStack(err)
	}
	for _, address := range adders {
		if address.Equal(*v.address) {
			return true, nil
		}
	}
	return false, nil
}

// AddVIP - 添加VIP
func (v *VipInterface) AddVIP() error {
	exist, err := v.IsExist()
	if err != nil {
		return err
	}
	// 不存在添加VIP
	if !exist {
		zlog.Debug("Add vip: " + v.String())
		if err := netlink.AddrAdd(v.link, v.address); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// DeleteVIP - 删除VIP
func (v *VipInterface) DeleteVIP() error {
	exist, err := v.IsExist()
	if err != nil {
		return err
	}
	// 存在则删除VIP
	if exist {
		zlog.Debug("Delete vip: " + v.String())
		if err := netlink.AddrDel(v.link, v.address); err != nil {
			return errors.WithStack(err)
		}
	}
	return nil
}

// VIP - 返回VIP地址
func (v *VipInterface) String() string {
	return v.address.IP.String()
}

// Interface - 返回网络接口名字
func (v *VipInterface) Interface() string {
	return v.link.Attrs().Name
}

func NewVip(vipAddr, iface string) (Vip, error) {
	// 解析vip
	if ip := net.ParseIP(vipAddr); ip.To4() == nil {
		return nil, errors.New(fmt.Sprintf("could not parse vip '%s'", vipAddr))
	}
	address, err := netlink.ParseAddr(vipAddr + "/32")
	if err != nil {
		return nil, errors.WithStack(err)
	}
	if iface == "lo" {
		address.Scope = unix.RT_SCOPE_HOST
	}

	// 连接网卡
	link, err := netlink.LinkByName(iface)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return &VipInterface{
		address: address,
		link:    link,
	}, nil
}
