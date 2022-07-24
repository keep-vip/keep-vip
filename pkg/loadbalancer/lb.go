package loadbalancer

import (
	"github.com/pkg/errors"
	"net"
)

type LoadBalancer struct {
	Name        string
	BindAddress *net.TCPAddr
	Type        string
	Backends    []*Backend
}

type Backend struct {
	Name    string
	Address *net.TCPAddr
}

var backendIndex int // 保存前一个端点的索引

func init() {
	backendIndex = -1 // 初始索引
}

// ReturnBackend - 返回一个后端
func (lb *LoadBalancer) ReturnBackend() (*net.TCPAddr, error) {
	if len(lb.Backends) == 0 {
		return nil, errors.New("No Backends configured")
	}
	if backendIndex < len(lb.Backends)-1 {
		backendIndex++
	} else {
		backendIndex = 0
	}
	return lb.Backends[backendIndex].Address, nil
}
