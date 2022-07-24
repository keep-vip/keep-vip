package main

import (
	"github.com/pkg/errors"
	"keep-vip/pkg/zlog"
)

func main() {
	err := errors.Errorf(
		"Check %s the protocol type is not supported: %s", "http", "tcp",
	)
	zlog.NewZapLog("debug", "console")
	zlog.Error(err)
}
