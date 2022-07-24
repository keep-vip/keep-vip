package setting

import (
	"github.com/pkg/errors"
	"github.com/spf13/viper"
)

var Config config

func LoadConfig(configPath string) error {
	if configPath == "" {
		return errors.New("config file path cannot be blank")
	}
	Viper := viper.New()
	Viper.SetConfigFile(configPath)
	Viper.SetConfigType("yaml")

	if err := Viper.ReadInConfig(); err != nil {
		return errors.WithStack(err)
	}
	if err := Viper.Unmarshal(&Config); err != nil {
		return errors.WithStack(err)
	}
	return nil
}
