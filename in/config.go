package main

import (
	"github.com/qjw/proxy/utils"
)

type Network struct {
	ServerHost string `json:"server_host" binding:"required"` // 服务器域名/IP
	ServerPort uint16 `json:"server_port" binding:"required"` // 服务器端口
	Bind       string `json:"bind" binding:"required"`        // 绑定的本地主机
	Port       uint16 `json:"port" binding:"required"`        // 绑定的本地端口
	Topic      string `json:"topic"`                          // 请求类别
}

type Config struct {
	Networks []*Network `json:"networks"`
}

func defaultConfig() *Config {
	return &Config{
		Networks: []*Network{
			{
				ServerHost: "127.0.0.1",
				ServerPort: 40001,
				Bind:       "127.0.0.1",
				Port:       40002,
				Topic:      utils.DftType,
			},
		},
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
