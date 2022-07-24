package network

import (
	"fmt"
	"github.com/pkg/errors"
	"keep-vip/pkg/zlog"
	"net"
	"time"
)

func LocalAddressIsExist(ip net.IP) (bool, error) {
	adders, err := net.InterfaceAddrs()
	if err != nil {
		return false, errors.WithStack(err)
	}
	for _, addr := range adders {
		localIP, ok := addr.(*net.IPNet)
		if ok && localIP.IP.Equal(ip) {
			return true, nil
		}
	}
	return false, nil
}

func CheckTcpAddress(address string, timeout int) error {
	tcpAddress, err := net.ResolveTCPAddr("tcp", address)
	if err != nil {
		return errors.WithStack(err)
	}
	// timeout低于1秒, 修改timeout为500毫秒
	var dialer net.Dialer
	if timeout <= 0 {
		dialer.Timeout = time.Millisecond * 500
	} else {
		dialer.Timeout = time.Second * time.Duration(int64(timeout))
	}
	zlog.Debug(fmt.Sprintf("Check address %s, timeout: %s", address, dialer.Timeout.String()))
	conn, err := dialer.Dial("tcp", tcpAddress.String())
	if err != nil {
		return errors.WithStack(err)
	}
	defer conn.Close()
	return nil
}
