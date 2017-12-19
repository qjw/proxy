package main

import (
	"github.com/qjw/proxy/utils"
)

type Config struct {
	Bind string `json:"bind" binding:"required"` // 绑定的本地主机
	Port uint16 `json:"port" binding:"required"` // 绑定的本地端口
}

func defaultConfig() *Config {
	return &Config{
		Bind: "127.0.0.1",
		Port: 40001,
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
