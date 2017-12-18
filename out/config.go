package main

import (
	"github.com/qjw/proxy/utils"
)

type Network struct {
	ServerHost  string `json:"server_host" binding:"required"`  // 服务器域名/IP
	ServerPort  uint16 `json:"server_port" binding:"required"`  // 服务器端口
	BackendHost string `json:"backend_host" binding:"required"` // 后端域名/IP
	BackendPort uint16 `json:"backend_port" binding:"required"` // 后端端口
	Topic       string `json:"topic"`                           // 请求类别
}

type Config struct {
	Networks          []*Network `json:"networks"`
	RetryInterval     int        `json:"retry_interval"`     // 掉线重试间隔(毫秒）
	HeartbeatInterval int        `json:"heartbeat_interval"` // 心跳检测间隔(毫秒）
	HeartbeatTimeout  int        `json:"heartbeat_timeout"`  // 心跳超时(毫秒）
}

func defaultConfig() *Config {
	return &Config{
		Networks: []*Network{
			{
				ServerHost:  "127.0.0.1",
				ServerPort:  40001,
				BackendHost: "127.0.0.1",
				BackendPort: 40003,
				Topic:       utils.DftType,
			},
		},
		RetryInterval:     utils.RetryInterval,
		HeartbeatInterval: utils.HeartbeatInterval,
		HeartbeatTimeout:  utils.HeartbeatTimeout,
	}
}

func parseConfig(path string) *Config {
	conf := defaultConfig()
	if err := utils.JsonConfToStruct(path, conf); err != nil {
		panic(err)
	}

	//	if err := kelly.Validate(conf); err != nil {
	//		panic(err)
	//	}

	return conf
}
