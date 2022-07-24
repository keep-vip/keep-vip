package cluster

import (
	"fmt"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"keep-vip/pkg/loadbalancer"
	"keep-vip/pkg/network"
	"keep-vip/pkg/zlog"
	"keep-vip/setting"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

type Cluster struct {
	RemotePeers  []RaftPeer
	LocalPeer    RaftPeer
	Vip          network.Vip
	stateMachine FSM
	stop         chan bool
	completed    chan bool
}

type RaftPeer struct {
	ID      string       // 集群内唯一标识
	Address *net.TCPAddr // IP地址
}

const RaftClusterNamespace = "keep_vip"

var (
	labels         = []string{"keep_vip_cluster", "server_id", "server_address"}
	MemberIsLeader = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: RaftClusterNamespace,
		Name:      "member_is_leader",
		Help:      "Whether or not this member is a leader. 1 if is, 0 otherwise",
	}, append(labels, "vip"))
	MemberState = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: RaftClusterNamespace,
		Name:      "member_state",
		Help:      "Member state, return Follower:0 Candidate:1 Leader:2",
	}, append(labels, "vip"))
	CheckPort = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: RaftClusterNamespace,
		Name:      "check_port",
		Help:      "Raft cluster check port. return 1 is success, 0 failure",
	}, append(labels, "name", "address"))
)

func InitCluster() (*Cluster, error) {
	if setting.Config.VIP == "" {
		return nil, errors.New("vip address config is empty")
	}

	// 必须使用root
	if os.Getuid() != 0 {
		return nil, errors.New("must run as root")
	}

	// 开启Prometheus
	if setting.Config.Prometheus.Enabled {
		prometheus.MustRegister(
			MemberIsLeader,
			MemberState,
			CheckPort,
		)
		http.Handle("/metrics", promhttp.Handler())
		tcpAddress, err := net.ResolveTCPAddr("tcp", setting.Config.Prometheus.Address)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		go func() {
			zlog.Info(fmt.Sprintf("Enabled prometheus at: http://%s/metrics", tcpAddress.String()))
			if err := http.ListenAndServe(tcpAddress.String(), nil); err != nil && err != http.ErrServerClosed {
				panic(err)
			}
		}()
	}

	vip, err := network.NewVip(setting.Config.VIP, setting.Config.Interface)
	if err != nil {
		return nil, err
	}

	return &Cluster{
		Vip: vip,
	}, nil
}

func ParseLevel(level string) hclog.Level {
	switch level {
	case "debug":
		return hclog.Debug
	case "info":
		return hclog.Info
	case "warn":
		return hclog.Warn
	case "error":
		return hclog.Error
	default:
		return hclog.Debug
	}
}

func (c *Cluster) StartRaftCluster(logLevel string) error {
	zlog.Info("Started")
	// 区分LocalPeer和RemotePeers
	if err := c.ClassifyRaftPeer(); err != nil {
		return err
	}
	// 本机Raft配置
	raftConfig := raft.DefaultConfig()
	raftConfig.LogLevel = ParseLevel(logLevel).String()
	raftConfig.LocalID = raft.ServerID(c.LocalPeer.ID)

	// 创建传输层
	transport, err := raft.NewTCPTransport(c.LocalPeer.Address.String(), c.LocalPeer.Address, 3, 10*time.Second, os.Stdout)
	if err != nil {
		return errors.WithStack(err)
	}

	// Raft存储
	snapshots := raft.NewInmemSnapshotStore()
	logStore := raft.NewInmemStore()
	stableStore := raft.NewInmemStore()

	// Raft集群配置
	clusterConfig := raft.Configuration{}
	// 添加本地端点
	clusterConfig.Servers = append(clusterConfig.Servers, raft.Server{
		ID:      raft.ServerID(c.LocalPeer.ID),
		Address: raft.ServerAddress(c.LocalPeer.Address.String()),
	})
	// 添加远端端点
	for _, peer := range c.RemotePeers {
		if c.LocalPeer.Address != peer.Address {
			clusterConfig.Servers = append(clusterConfig.Servers, raft.Server{
				ID:      raft.ServerID(peer.ID),
				Address: raft.ServerAddress(peer.Address.String()),
			})
		}
	}
	// 引导集群
	if err := raft.BootstrapCluster(raftConfig, logStore, stableStore, snapshots, transport, clusterConfig); err != nil {
		return errors.WithStack(err)
	}

	// 创建Raft
	raftServer, err := raft.NewRaft(raftConfig, c.stateMachine, logStore, stableStore, snapshots, transport)
	if err != nil {
		return errors.WithStack(err)
	}
	zlog.Info("This instance will wait approximately 5 seconds, from cold start to ensure cluster elections are complete")
	time.Sleep(time.Second * 5)

	// 添加负载均衡
	lbManager := loadbalancer.NewLBManager()
	for _, confLB := range setting.Config.LoadBalancers {
		bindAddress, err := net.ResolveTCPAddr("tcp", confLB.BindAddress)
		if err != nil {
			return errors.WithStack(err)
		}
		lb := &loadbalancer.LoadBalancer{
			Name:        confLB.Name,
			Type:        confLB.Type,
			BindAddress: bindAddress,
		}
		for _, backend := range confLB.Backends {
			address, err := net.ResolveTCPAddr("tcp", backend.Address)
			if err != nil {
				return errors.WithStack(err)
			}
			lb.Backends = append(lb.Backends, &loadbalancer.Backend{
				Name:    backend.Name,
				Address: address,
			})
		}
		if err := lbManager.AddLoadBalancer(lb); err != nil {
			return err
		}
		zlog.Info(fmt.Sprintf("Load Balancer [%s] started, connection address: %s:%d",
			lb.Name, c.Vip.String(), bindAddress.Port))
	}

	// 检查集群状态
	ticker := time.NewTicker(time.Second * time.Duration(setting.Config.ChecksInterval))
	c.stop = make(chan bool, 1)
	c.completed = make(chan bool, 1)
	var isLeader bool
	go func() {
		for {
			select {
			case leader := <-raftServer.LeaderCh():
				// Leader节点绑定VIP, 广播ARP
				if leader {
					zlog.Info("This node is Leader of the cluster")
					isLeader = true
					// 添加VIP
					if err := c.Vip.AddVIP(); err != nil {
						zlog.Warn(err.Error())
					}
					// 广播ARP
					if err := network.ARPSendGratuitous(c.Vip.String(), c.Vip.Interface()); err != nil {
						zlog.Warn(err.Error())
					}
					c.PromMemberIsLeader(1)
				} else {
					isLeader = false
					zlog.Info("This node is becoming a follower within the cluster")
					// 删除VIP
					if err := c.Vip.DeleteVIP(); err != nil {
						zlog.Warn(err.Error())
					}
					c.PromMemberIsLeader(0)
				}
			case <-ticker.C:
				// 定时检查, 如果节点是Leader, VIP没有绑定则添加VIP, 发送ARP
				zlog.Info(fmt.Sprintf("Start Check %s", raftServer.String()))
				leaderAddr, leaderID := raftServer.LeaderWithID()
				if string(leaderAddr) != "" {
					zlog.Debug("Leader is " + string(leaderAddr))
				}
				// Check VIP
				if c.LocalPeer.Address.String() == string(leaderAddr) && c.LocalPeer.ID == string(leaderID) {
					isLeader = true
					// 添加VIP
					if err := c.Vip.AddVIP(); err != nil {
						zlog.Warn(err.Error())
					}
					// 广播ARP
					if err := network.ARPSendGratuitous(c.Vip.String(), c.Vip.Interface()); err != nil {
						zlog.Error(err)
					}
					c.PromMemberIsLeader(1)
				} else {
					isLeader = false
					if err := c.Vip.DeleteVIP(); err != nil {
						zlog.Warn(err.Error())
					}
					c.PromMemberIsLeader(0)
				}

				// Prom State
				switch raftServer.State().String() {
				case "Follower":
					c.PromMemberState(0)
				case "Candidate":
					c.PromMemberState(1)
				case "Leader":
					c.PromMemberState(2)
				}

				// Check Port
				for _, check := range setting.Config.Checks {
					check := check
					go func() {
						zlog.Info("Open check port: " + check.Name)
						switch strings.ToLower(check.Protocol) {
						case "tcp":
							if check.Timeout > setting.Config.ChecksInterval {
								check.Timeout = 0
							}
							if err := network.CheckTcpAddress(check.Address, check.Timeout); err != nil {
								zlog.Error(err)
								// 如果是Leader, 则退出程序
								if isLeader {
									c.Stop()
									os.Exit(1)
								}
								// Prom
								c.PromCheckPort(check.Name, check.Address, 0)
							} else {
								// Prom
								c.PromCheckPort(check.Name, check.Address, 1)
							}
						default:
							zlog.Error(errors.Errorf(
								"Check port %s the protocol type is not supported: %s",
								check.Name,
								check.Protocol,
							))
						}
					}()
				}
				// TODO Check LB Backend

			case <-c.stop:
				leaderAddr, leaderID := raftServer.LeaderWithID()
				if c.LocalPeer.Address.String() == string(leaderAddr) && c.LocalPeer.ID == string(leaderID) {
					// 删除VIP
					if err := c.Vip.DeleteVIP(); err != nil {
						zlog.Warn(err.Error())
					}
				}

				// 关闭负载均衡
				zlog.Info("Stopping Load Balancers")
				lbManager.StopAll()

				// 关闭RAFT
				zlog.Info("Stopping Raft Cluster")
				if err := raftServer.Shutdown().Error(); err != nil {
					zlog.Error(err)
				}

				// 等待关闭
				zlog.Info("Wait Stopping 3s")
				time.Sleep(time.Second * 3)
				close(c.completed)
				return
			}
		}
	}()
	return nil
}

func (c *Cluster) ClassifyRaftPeer() error {
	for _, member := range setting.Config.Members {
		address, err := net.ResolveTCPAddr("tcp", member.Address)
		if err != nil {
			return err
		}
		// 区分LocalPeer和RemotePeers
		exist, err := network.LocalAddressIsExist(address.IP)
		if err != nil {
			return err
		}
		if exist {
			// LocalPeer
			c.LocalPeer = RaftPeer{ID: member.ID, Address: address}
		} else {
			// RemotePeers
			c.RemotePeers = append(c.RemotePeers, RaftPeer{
				ID:      member.ID,
				Address: address,
			})
		}
	}
	return nil
}

func (c *Cluster) PromMemberIsLeader(current float64) {
	MemberIsLeader.With(prometheus.Labels{
		"keep_vip_cluster": setting.Config.Cluster,
		"server_id":        c.LocalPeer.ID,
		"server_address":   c.LocalPeer.Address.String(),
		"vip":              c.Vip.String(),
	}).Set(current)
}

func (c *Cluster) PromMemberState(current float64) {
	MemberState.With(prometheus.Labels{
		"keep_vip_cluster": setting.Config.Cluster,
		"server_id":        c.LocalPeer.ID,
		"server_address":   c.LocalPeer.Address.String(),
		"vip":              c.Vip.String(),
	}).Set(current)
}

func (c *Cluster) PromCheckPort(name, address string, current float64) {
	CheckPort.With(prometheus.Labels{
		"keep_vip_cluster": setting.Config.Cluster,
		"server_id":        c.LocalPeer.ID,
		"server_address":   c.LocalPeer.Address.String(),
		"name":             name,
		"address":          address,
	}).Set(current)
}

func (c *Cluster) Stop() {
	// 关闭停止通道，这将关闭VIP和负载均衡
	close(c.stop)
	// 等待直到完成
	<-c.completed

	zlog.Info("Stopped")
}
