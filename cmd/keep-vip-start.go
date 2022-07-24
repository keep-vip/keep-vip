package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"keep-vip/cluster"
	"keep-vip/pkg/zlog"
	"keep-vip/setting"
	"os"
	"os/signal"
	"syscall"
)

var configPath string

func init() {
	keepVipStart.Flags().StringVarP(&configPath, "config", "c", "", "Path to a keep-vip configuration")
}

var keepVipStart = &cobra.Command{
	Use:   "start",
	Short: "Start the Virtual IP / Load balancer",
	Run: func(cmd *cobra.Command, args []string) {
		// 设置日志级别和编码
		zlog.NewZapLog(logLevel, logEncoder)

		// 加载配置文件
		if err := setting.LoadConfig(configPath); err != nil {
			zlog.Error(errors.WithMessage(err, "example: keep-vip start -c ./config.yaml"))
			return
		}

		// 初始化集群
		newCluster, err := cluster.InitCluster()
		if err != nil {
			zlog.Error(err)
			return
		}

		// 启动Raft
		if err := newCluster.StartRaftCluster(logLevel); err != nil {
			zlog.Error(err)
			return
		}

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGCHLD)
		<-quit

		newCluster.Stop()
	},
}
