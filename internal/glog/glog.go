package glog

import (
	"log/slog"
	"os"

	"github.com/Fishmansky/glog/internal/client"
	"github.com/Fishmansky/glog/internal/server"
	"github.com/spf13/viper"
)

func SetConfigDefaults() {
	viper.SetDefault("logdir", "/var/log/glog")
	viper.SetDefault("mainlog", "glog.log")
	viper.AddConfigPath("/etc/glog")
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
}

type Glog interface {
	Run()
}

func NewGlog() Glog {
	SetConfigDefaults()
	err := viper.ReadInConfig()
	if err != nil {
		slog.Error("Configuration file not found!")
		os.Exit(1)
	}
	mode := viper.GetString("mode")
	if mode == "" {
		slog.Error("mode variable not set in config file!")
		os.Exit(1)
	}
	if mode == "server" {
		return server.New()
	} else if mode == "client" {
		return client.New()
	}
	return nil
}
