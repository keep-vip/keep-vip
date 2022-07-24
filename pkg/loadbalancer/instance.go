package loadbalancer

import (
	"fmt"
	"github.com/pkg/errors"
	"io"
	"keep-vip/pkg/zlog"
	"net"
	"sync"
	"time"
)

type LBInstance struct {
	stop         chan bool
	stopped      chan bool
	LoadBalancer *LoadBalancer
}

func (li *LBInstance) startTCP() error {
	listen, err := net.ListenTCP("tcp", li.LoadBalancer.BindAddress)
	if err != nil {
		return errors.WithStack(err)
	}
	go func() {
		for {
			select {
			case <-li.stop:
				listen.Close()
				close(li.stopped)
			default:
				if err = listen.SetDeadline(time.Now().Add(200 * time.Millisecond)); err != nil {
					zlog.Error(errors.Errorf("Error setting TCP deadline [%v]", err))
				}
				fd, err := listen.Accept()
				if err != nil {
					if opErr, ok := err.(*net.OpError); ok && opErr.Timeout() {
						continue
					} else if err != io.EOF {
						zlog.Error(errors.Errorf("TCP Accept error [%s]", err))
					}
				}
				go tcpConnect(fd, li.LoadBalancer)
			}
		}
	}()
	return nil
}

func (li *LBInstance) Stop() {
	close(li.stop)

	<-li.stopped
	zlog.Debug(fmt.Sprintf("Load Balancer instance [%s] has stopped", li.LoadBalancer.Name))
}

func tcpConnect(frontend net.Conn, lb *LoadBalancer) {
	defer frontend.Close()

	// 设置超时时间
	dialer := net.Dialer{Timeout: time.Millisecond * 500}

	// 获取后端地址
	var endpoint net.Conn
	for {
		backend, err := lb.ReturnBackend()
		if err != nil {
			zlog.Error(err)
			return
		}
		// 拨号
		endpoint, err = dialer.Dial("tcp", backend.String())
		if err != nil {
			zlog.Warn(fmt.Sprintf("[%s]---X [FAILED] X-->[%s]", frontend.RemoteAddr(), backend.String()))
			zlog.Error(err)
		} else {
			zlog.Debug(fmt.Sprintf("[%s]--->[ACCEPT]--->[%s]", frontend.RemoteAddr(), backend.String()))
			defer endpoint.Close()
			break
		}
	}

	wg := &sync.WaitGroup{}
	wg.Add(1)
	// frontend -> to an endpoint
	go func() {
		if _, err := io.Copy(endpoint, frontend); err != nil {
			zlog.Error(errors.Errorf("Error sending data to endpoint [%s] [%v]", endpoint.RemoteAddr(), err))
		}
		wg.Done()
	}()
	// endpoint -> back to frontend
	if _, err := io.Copy(frontend, endpoint); err != nil {
		zlog.Error(errors.Errorf("Error sending data to frontend [%s] [%s]", frontend.RemoteAddr(), err))
	}
	wg.Wait()
}
