# Keep-vip

---
本项目参考[kube-vip v0.3.9](https://github.com/kube-vip/kube-vip)开发。使用RAFT算法选举Leader并指定网卡绑定vip，发送arp广播报文
- 增加了端口检查功能。检查失败，程序会直接退出。需要使用systemctl配置Restart=always或者docker配置--restart=always，提供重启策略
- 支持Prometheus监控

## 一. 配置文件

```yaml
cluster: cluster-01
interface: ens33
vip: 172.16.0.100             # 仅支持ipv4
ChecksInterval: 2             # 单位s, 发送Gratuitous ARP、Checks间隔, 大于checks超时时间
prometheus:
  enabled: true               # 开启Prometheus
  address: 0.0.0.0:9195
# 检查端口, 如果检查失败会退出程序. 触发选举, 需要配置重启策略
checks:
  - name: http_80
    protocol: tcp             # 目前仅支持tcp
    address: 127.0.0.1:80     # 127.0.0.1:80
    timeout: 1                # 检查端口超时时间
members:
  - id: server1
    address: 172.16.0.11:20000
  - id: server2
    address: 172.16.0.12:20000
  - id: server3
    address: 172.16.0.13:20000
loadBalancers:               # 负载均衡, 选填
  - name: NginxLB
    bindAddress: 0.0.0.0:8080
    type: tcp
    backends:
      - name: nginx-01
        address: 172.16.0.11:80
      - name: nginx-02
        address: 172.16.0.12:80
      - name: nginx-03
        address: 172.16.0.13:80
```

## 二. Systemd
```shell
cat <<- 'EOF' > /usr/lib/systemd/system/keep-vip.service
[Unit]
Description=Keep Vip
After=network.target
Wants=network.target
[Service]
ExecStart=/bin/keep-vip start -c /etc/keep-vip/config.yaml
Restart=always
RestartSec=3s
SyslogIdentifier=keep-vip
StandardOutput=syslog
StandardError=syslog
OOMScoreAdjust=-1000
[Install]
WantedBy=multi-user.target
EOF
```
- Rsyslog 日志过滤重定向
```shell
# rsyslog默认会将特殊字符\t转换成#009
echo '$EscapeControlCharactersOnReceive off' >> /etc/rsyslog.conf
# 配置文件
cat <<- 'EOF' >  /etc/rsyslog.d/keep-vip_log.conf
if $programname == 'keep-vip' then /var/log/keep-vip.log
& stop
EOF
# 检查语法
rsyslogd -N1 -f /etc/rsyslog.d/keep-vip_log.conf
# 重启rsyslog
systemctl restart rsyslog
systemctl status rsyslog
```
- Logrotate 日志切割

  - copytruncate：把正在输出的日志拷(copy)一份出来，再清空(trucate)原来的日志
```shell
/var/log/keep-vip.log {
    daily
    compress
    maxsize 30M
    copytruncate
    rotate 5
}
```

## 三. 部署方案

### 1. VIP + Haproxy / Nginx

#### 1. 方案介绍

Keep-vip使用RAFT算法选举Leader并指定网卡绑定vip，配合负载均衡器(Haproxy或者Nginx)来代理后端服务。keep-vip配置端口监测，检查负载均衡器监听的端口和负载均衡器实现共生关系。如果检查失败，则退出程序，触发RAFT选举。

注意:  检查失败，程序会直接退出。需要使用systemctl配置Restart=always或者docker配置--restart=always，提供重启策略

![keep-vip-haproxy](https://github.com/keep-vip/keep-vip/blob/main/assets/keep-vip-haproxy.png)


### 2. VIP + Load Balancer

#### 1. 方案介绍

Keep-vip 使用Go语言实现了负载均衡，支持tcp、udp、http协议。实现了4层和7层代理，直接代理后端服务。不需要在使用nginx或者haproxy来做负载均衡器

![keep-vip-lb](https://github.com/keep-vip/keep-vip/blob/main/assets/keep-vip-lb.png)


## 四. Leader选举

![raft-status](https://github.com/keep-vip/keep-vip/blob/main/assets/raft-status.png)


- Leader退出会先成为Follower, 停止复制集。如果未收到新的Leader的心跳。会成为Candidate，请求投票。直到成为Leader或者收到其他Leader的心跳包成为Follower
- Follower收到心跳包超时，会成为Candidate。发送投票请求，直到成为Leader或者收到其他Leader的心跳包，成为Follower
- 每一次选举都会有一个任期号，使用连续的整数标记，一个节点在一个任期内最多只能投一票。收到Leader心跳包会退出选举
- 为了避免选票被无限的重复瓜分，Raft算法使用选举随机超时。为了阻止选票起初就被瓜分，选举超时时间是从一个固定的区间(例如150-300毫秒)随机选择

```bash
# Raft算法中服务器节点之间通信使用RPC调用，基本的一致性算法只需要两种类型的RPC

AppendEntries: Leader节点使用该消息向其他节点同步日志, 或者发送空消息作为心跳包以维持leader的统治地位
请求参数:
	- term: 当前leader节点的term值
	- leaderId: 当前leader节点的编号（注：follower根据领导者id把客户端的请求重定向到领导者，比如有时客户端把请求发给了follower而不是leader）
	- prevLogIndex: 当前发送的日志的前面一个日志的索引
	- prevLogTerm: 当前发送的日志的前面一个日志的term值 （这个和上一个作用是follower日志有效性检查）
	- entries[]: 需要各个节点存储的日志条目(用作心跳包时为空, 可能会出于效率发送超过一个日志条目)
	- leaderCommit: 当前leader节点最高的被提交的日志的索引(就是leader节点的commitIndex)
返回值:
	- term: 接收日志节点的term值, 主要用来更新当前leader节点的term值
	- success: 如果接收日志节点的log[]结构中prevLogIndex索引处含有日志并且该日志的term等于prevLogTerm则返回true, 否则返回false

RequestVote：Candidate节点请求其他节点投票给自己
请求参数:
    - term: 当前candidate节点的term值
    - candidateId: 当前candidate节点的编号
    - lastLogIndex: 当前candidate节点最后一个日志的索引
    - lastLogTerm: 当前candidate节点最后一个日志的term值
返回值:
	- term: 接受投票节点的term值, 主要用来更新当前candidate节点的term值
	- voteGranted: 是否给该申请节点投票
```

## 五. 脑裂问题

### 1. 什么是脑裂

在一个集群中任何时刻只能有一个Leader。当集群中同时有多个Leader，被称之为脑裂(brain split)
- 什么情况下会发生脑裂?
  - 当集群间网络发生故障，相互之间网络无法通讯，无法发送心跳包。导致脑裂发生
- 在raft中，使用两点保证了不发生脑裂风
  - 一个节点，在一个任期内最多只能投一票
  - 只有获得多数投票的节点才会成为Leader

### 2. 故障测试

![brain-split](https://github.com/keep-vip/keep-vip/blob/main/assets/brain-split-7475829.png)


- 使用iptables模拟网络故障

| raft节点       | ip地址      |
| -------------- | ----------- |
| server1 leader | 172.16.0.11 |
| server2        | 172.16.0.12 |
| server3        | 172.16.0.13 |

```bash
# 清除规则
iptables -P INPUT    ACCEPT
iptables -P OUTPUT   ACCEPT
iptables -P FORWARD  ACCEPT
iptables -F
iptables -X
iptables -Z

iptables -A INPUT -s 172.16.0.3/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.11/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.12/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j ACCEPT

# 默认规则
iptables -P INPUT   DROP
iptables -P FORWARD DROP
iptables -P OUTPUT  ACCEPT
iptables -L -n -v
```

- server1 leader x---x server2 || server3。server1跟server2或server3其中一个失联

![brain-split](https://github.com/keep-vip/keep-vip/blob/main/assets/brain-split.png)


```bash
# server1执行, 断开server2的连接
iptables -D INPUT -s 172.16.0.12/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.12/32 -p all -j DROP
# server1
	raft: failed to contact: server-id=server2 time=500.465752ms
	raft: failed to heartbeat to: peer=172.16.0.12:10000 error="msgpack decode error [pos 75166]: read tcp 172.16.0.11:57738->172.16.0.12:10000: i/o timeout"
# server2
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.11:10000 # 心跳超时,开始选举
    raft: entering candidate state: node="Node at 172.16.0.12:10000 [Candidate]" term=6 # 成为候选人
    raft: votes: needed=2 # 需要两个选票
    raft: vote granted: from=server2 term=6 tally=1 # server2自己投了一票
    raft: Election timeout reached, restarting election # 选举超时, 重新选举
    raft: failed to make requestVote RPC: target="{Voter server1 172.16.0.11:10000}" error="dial tcp 172.16.0.11:10000: i/o timeout" # 向server1请求选票失败
# server3
	 raft: rejecting vote request since we have a leader: from=172.16.0.12:10000 leader=172.16.0.11:10000 # 拒绝投票请求，因为已经有leader
# 结果
Leader 未发生漂移, 对集群无影响继续运行

```

- server1 leader x---x server2 && server3。server1同时跟server2和server3失联

![brain-split](https://github.com/keep-vip/keep-vip/blob/main/assets/brain-split-7475950.png)


```bash
# server1执行, 断开server2和server3的连接
iptables -D INPUT -s 172.16.0.12/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.12/32 -p all -j DROP
iptables -D INPUT -s 172.16.0.13/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j DROP
# server1
	raft: failed to contact: server-id=server2 time=515.286845ms
	raft: failed to contact: server-id=server3 time=501.969798ms
	raft: failed to contact quorum of nodes, stepping down # 联系节点仲裁失败，退出leader
	raft: entering follower state: follower="Node at 172.16.0.11:10000 [Follower]" leader= # 成为follower
	raft: aborting pipeline replication: peer="{Voter server2 172.16.0.12:10000}" # 停止复制集
	raft: aborting pipeline replication: peer="{Voter server3 172.16.0.13:10000}"
	New Election event
	This node is becoming a follower within the cluster
	raft: heartbeat timeout reached, starting election: last-leader= # 心跳超时,开始选举
	raft: heartbeat timeout reached, starting election: last-leader=
    raft: entering candidate state: node="Node at 172.16.0.11:10000 [Candidate]" term=4
    raft: votes: needed=2
    raft: vote granted: from=server1 term=4 tally=1
    raft: Election timeout reached, restarting election
	raft: failed to heartbeat to: peer=172.16.0.12:10000 error="msgpack decode error [pos 13393]: read tcp 172.16.0.11:57790->172.16.0.12:10000: i/o timeout"
	raft: failed to heartbeat to: peer=172.16.0.13:10000 error="msgpack decode error [pos 12272]: read tcp 172.16.0.11:42690->172.16.0.13:10000: i/o timeout"
# server2
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.11:10000  # 心跳超时开始选举
    raft: entering candidate state: node="Node at 172.16.0.12:10000 [Candidate]" term=4 # 成为候选者
    raft: votes: needed=2 # 需要两个选票
    raft: vote granted: from=server2 term=4 tally=1 # 收到server2一个选票
    raft: duplicate requestVote for same term: term=4
    raft: Election timeout reached, restarting election # 选举超时重新选举
    raft: entering candidate state: node="Node at 172.16.0.12:10000 [Candidate]" term=5
    raft: votes: needed=2  # 需要两个选票
    raft: vote granted: from=server2 term=5 tally=1 # 收到server2一个选票
    raft: vote granted: from=server3 term=5 tally=2 # 收到server3一个选票
    raft: election won: tally=2
    raft: entering leader state: leader="Node at 172.16.0.12:10000 [Leader]" # server2成为Leader
    raft: added peer, starting replication: peer=server1 # 添加server1为复制集
    raft: added peer, starting replication: peer=server3 # 添加server3为复制集
    The Node [172.16.0.12:10000] is leading
    raft: failed to contact: server-id=server1 time=5.142161005
    raft: failed to make requestVote RPC: target="{Voter server1 172.16.0.11:10000}" error="dial tcp 172.16.0.11:10000: i/o timeout"
    raft: failed to appendEntries to: peer="{Voter server1 172.16.0.11:10000}" error="dial tcp 172.16.0.11:10000: i/o timeout"
# server3
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.11:10000 # 心跳超时开始选举
    raft: entering candidate state: node="Node at 172.16.0.13:10000 [Candidate]" term=4 # 成为候选者
    raft: votes: needed=2	# 需要2个选票
    raft: vote granted: from=server3 term=4 tally=1
    raft: lost leadership because received a requestVote with a newer term # 退出选举，因为收到新的term
    raft: entering follower state: follower="Node at 172.16.0.13:10000 [Follower]" leader= # 成为追随者
    The Node [172.16.0.12:10000] is leading
# 结果
Leader切换节点，Vip发生漂移, server2和server3其中一个节点会选举成为Leader。会发生大约2秒切换时间，集群继续运行

```

- server1 leader x---x server2 && server3，server2 x---x server3。server1同时跟server2和server3失联，此时server2和server3也失联

![brain-split](https://github.com/keep-vip/keep-vip/blob/main/assets/brain-split-7476018.png)


```bash
# server1执行, 断开server2和server3的连接
iptables -D INPUT -s 172.16.0.12/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.12/32 -p all -j DROP
iptables -D INPUT -s 172.16.0.13/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j DROP
# server2执行
iptables -D INPUT -s 172.16.0.13/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j DROP
# server1
	The Node [172.16.0.11:10000] is leading
    raft: failed to contact: server-id=server2 time=553.800004ms
    raft: failed to contact: server-id=server3 time=500.91959ms
    raft: failed to contact quorum of nodes, stepping down # 联系节点仲裁失败，退出leader
    raft: entering follower state: follower="Node at 172.16.0.11:10000 [Follower]" leader= # 成为追随者
    raft: aborting pipeline replication: peer="{Voter server2 172.16.0.12:10000}"  # 停止复制集
    raft: aborting pipeline replication: peer="{Voter server3 172.16.0.13:10000}"
    New Election event
    This node is becoming a follower within the cluster
    raft: heartbeat timeout reached, starting election: last-leader=
    raft: entering candidate state: node="Node at 172.16.0.11:10000 [Candidate]" term=6
    raft: votes: needed=2
    raft: vote granted: from=server1 term=6 tally=1
    raft: Election timeout reached, restarting election
    raft: failed to heartbeat to: peer=172.16.0.12:10000 error="msgpack decode error [pos 18054]: read tcp 172.16.0.11:58014->172.16.0.12:10000: i/o timeout" # 心跳失败
	raft: failed to heartbeat to: peer=172.16.0.13:10000 error="msgpack decode error [pos 17582]: read tcp 172.16.0.11:42910->172.16.0.13:10000: i/o timeout"
	raft: failed to make requestVote RPC: target="{Voter server3 172.16.0.13:10000}" error="dial tcp 172.16.0.13:10000: i/o timeout" 请求投票失败
	raft: failed to make requestVote RPC: target="{Voter server2 172.16.0.12:10000}" error="dial tcp 172.16.0.12:10000: i/o timeout"
# server2
	The Node [172.16.0.11:10000] is leading
    raft: rejecting vote request since we have a leader: from=172.16.0.13:10000 leader=172.16.0.11:10000 # 拒绝投票请求，因为已经有leader
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.11:10000 # 心跳超时
    raft: entering candidate state: node="Node at 172.16.0.12:10000 [Candidate]" term=6 # 成为候选人
    raft: votes: needed=2
    raft: vote granted: from=server2 term=6 tally=1
    raft: lost leadership because received a requestVote with a newer term # 退出leader选举，因为收到新的term
    raft: entering follower state: follower="Node at 172.16.0.12:10000 [Follower]" leader= # 成为追随者
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.13:10000 # 心跳超时
    raft: entering candidate state: node="Node at 172.16.0.12:10000 [Candidate]" term=8 # 成为候选者
    raft: votes: needed=2
    raft: vote granted: from=server2 term=8 tally=1
    raft: Election timeout reached, restarting election
# server3
	The Node [172.16.0.11:10000] is leading
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.11:10000 # 心跳超时
    raft: entering candidate state: node="Node at 172.16.0.13:10000 [Candidate]" term=6 # 成为候选者
    raft: votes: needed=2
    raft: vote granted: from=server3 term=6 tally=1
    raft: duplicate requestVote for same term: term=6
    raft: Election timeout reached, restarting election # 选举超时
    raft: entering candidate state: node="Node at 172.16.0.13:10000 [Candidate]" term=7
    raft: votes: needed=2
    raft: vote granted: from=server3 term=7 tally=1 # 收到server3
    raft: vote granted: from=server2 term=7 tally=2 # 收到server2
    raft: election won: tally=2
    raft: entering leader state: leader="Node at 172.16.0.13:10000 [Leader]" # 成为Leader
    raft: added peer, starting replication: peer=server1 # 添加复制集
    raft: added peer, starting replication: peer=server2
    New Election event
    This node is assuming leadership of the cluster
    The Node [172.16.0.13:10000] is leading
    raft: pipelining replication: peer="{Voter server2 172.16.0.12:10000}"
    raft: failed to contact: server-id=server1 time=2.033247789s
    raft: failed to contact: server-id=server2 time=500.406381ms

    New Election event
    This node is becoming a follower within the cluster
    raft: failed to contact quorum of nodes, stepping down
    raft: entering follower state: follower="Node at 172.16.0.13:10000 [Follower]" leader= # 成为Follower
    raft: aborting pipeline replication: peer="{Voter server2 172.16.0.12:10000}"
    raft: lost leadership because received a requestVote with a newer term

    raft: heartbeat timeout reached, starting election: last-leader=
    raft: entering candidate state: node="Node at 172.16.0.13:10000 [Candidate]" term=9 # 成为Candidate
    raft: votes: needed=2
    raft: vote granted: from=server3 term=9 tally=1
    raft: Election timeout reached, restarting election
# 结果
Leader选举失败, 集群故障。每个节点最终都会成为候选者，陷入请求选票的死循环

```

- server2 x---x server3，server2和server3失联

![brain-split](https://github.com/keep-vip/keep-vip/blob/main/assets/brain-split-7476065.png)

```bash
# server2执行
iptables -D INPUT -s 172.16.0.13/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j DROP

# 结果
server1、server2、server3 无异常日志。集群正常运行，尽快修复故障
```

- server2 x---x server3，server1 leader x---x server3。server2和server3失联。此时server1 leader和server3也失联

![brain-split](https://github.com/keep-vip/keep-vip/blob/main/assets/brain-split-7476147.png)

```bash
# server2执行
iptables -D INPUT -s 172.16.0.13/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j DROP

# server1执行
iptables -D INPUT -s 172.16.0.13/32 -p all -j ACCEPT
iptables -A INPUT -s 172.16.0.13/32 -p all -j DROP

# server1
    The Node [172.16.0.11:10000] is leading
    raft: failed to contact: server-id=server3 time=501.587723ms
# server2
	The Node [172.16.0.11:10000] is leading
# server3
    The Node [172.16.0.11:10000] is leading
    raft: heartbeat timeout reached, starting election: last-leader=172.16.0.11:10000
    raft: entering candidate state: node="Node at 172.16.0.13:10000 [Candidate]" term=4
    raft: votes: needed=2
    raft: vote granted: from=server3 term=4 tally=1
    raft: Election timeout reached, restarting election
# 结果
Leader 未发生漂移, 对集群无影响继续运行。server3会陷入投票循环
```

- 结论
  - 三个节点，不会发生脑裂。失联或故障一个节点，集群正常运行。失联或故障两个节点，集群运行异常，每个节点最终都会成为候选者，陷入请求选票的死循环
  - 五个节点，理论上可以故障两个节点。但是增加了脑裂的风险，有可能其中多个节点同时收到3个选票。此结论并未实验验证
