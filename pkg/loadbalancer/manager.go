package loadbalancer

import (
	"github.com/pkg/errors"
	"strings"
)

type LBManager struct {
	LBInstances []*LBInstance
}

func NewLBManager() LBManager {
	return LBManager{}
}

func (lm *LBManager) AddLoadBalancer(lb *LoadBalancer) error {
	lbInstance := &LBInstance{
		stop:         make(chan bool, 1),
		stopped:      make(chan bool, 1),
		LoadBalancer: lb,
	}
	switch strings.ToLower(lb.Type) {
	case "tcp":
		if err := lbInstance.startTCP(); err != nil {
			return err
		}
	default:
		return errors.Errorf(
			"Add LoadBalancer %s the protocol type is not supported: %s",
			lb.Name,
			lb.Type,
		)
	}
	lm.LBInstances = append(lm.LBInstances, lbInstance)
	return nil
}

func (lm *LBManager) StopAll() {
	for _, lb := range lm.LBInstances {
		lb.Stop()
	}
}
