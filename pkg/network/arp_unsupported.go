//go:build !linux
// +build !linux

package network

import "fmt"

// ARPSendGratuitous 只支持Linux, 所以返回错误
func ARPSendGratuitous(address, ifaceName string) error {
	return fmt.Errorf("unsupported on this OS")
}
