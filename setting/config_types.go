package setting

type config struct {
	Cluster        string          // 集群名称
	Interface      string          // 绑定到的网络接口(默认:First Adapter)
	VIP            string          // VIP地址
	ChecksInterval int             // 单位s, 发送Gratuitous ARP间隔
	Prometheus     prometheus      // Prometheus
	Checks         []check         // 检查端口
	Members        []member        // 集群内成员
	LoadBalancers  []loadBalancers // 负载均衡
}

type prometheus struct {
	Enabled bool
	Address string
}

type check struct {
	Name     string
	Address  string
	Protocol string
	Timeout  int
}

type member struct {
	ID      string // 集群内唯一标识
	Address string // IP地址
}

type loadBalancers struct {
	Name        string
	BindAddress string
	Type        string
	Backends    []struct {
		Name    string
		Address string
	}
}
