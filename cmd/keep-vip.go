package cmd

import (
	"fmt"
	"github.com/spf13/cobra"
	"os"
)

var keepVipCmd = &cobra.Command{
	Use:   "keep-vip",
	Short: "This is a server for providing a Virtual IP and load-balancer",
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

var (
	logEncoder string
	logLevel   string
)

func init() {
	// 命令行参数
	keepVipCmd.PersistentFlags().StringVar(&logEncoder, "log-encoder", "console", "Output Encoder. One of: [console|json]")
	keepVipCmd.PersistentFlags().StringVar(&logLevel, "log-level", "debug", "Log Level. Ond of: [debug|info|warn|error]")

	// 添加子命令
	keepVipCmd.AddCommand(keepVipStart)
}

// Execute - 命令解析
func Execute() {
	if err := keepVipCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
