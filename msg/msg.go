package msg

import (
	"encoding/json"
	"reflect"
)

var TypeMap map[string]reflect.Type

func init() {
	TypeMap = make(map[string]reflect.Type)

	t := func(obj interface{}) reflect.Type { return reflect.TypeOf(obj).Elem() }
	TypeMap["OutRequest"] = t((*OutRequest)(nil))
	TypeMap["OutDataRequest"] = t((*OutDataRequest)(nil))
	TypeMap["DataActiveRequest"] = t((*DataActiveRequest)(nil))
	TypeMap["InRequest"] = t((*InRequest)(nil))
	TypeMap["NewDataRequest"] = t((*NewDataRequest)(nil))
	TypeMap["Response"] = t((*Response)(nil))
	TypeMap["Ping"] = t((*Ping)(nil))
	TypeMap["Pong"] = t((*Pong)(nil))
}

type Message interface{}

type Envelope struct {
	Type    string
	Payload json.RawMessage
}

// 新的上游管理通道
type OutRequest struct {
	Magic   string `json:"magic"`
	Version string `json:"version"`
	Type    string `json:"type"`
}

// 请求新的上游数据通道
type NewDataRequest struct {
	Magic string `json:"magic"`
	Type  string `json:"type"`
}

// 数据通道生效请求
type DataActiveRequest struct {
	Magic string `json:"magic"`
	Type  string `json:"type"`
}

// 新的上游数据通道
type OutDataRequest struct {
	Magic   string `json:"magic"`
	Version string `json:"version"`
	Type    string `json:"type"`
}

// 新的下游数据通道
type InRequest struct {
	Magic   string `json:"magic"`
	Version string `json:"version"`
	Type    string `json:"type"`
}

// 相应
type Response struct {
	Magic   string `json:"magic"`
	Request string `json:"request"`
	Message string `json:"message"`
}

// A client or server may send this message periodically over
// the control channel to request that the remote side acknowledge
// its connection is still alive. The remote side must respond with a Pong.
type Ping struct {
}

// Sent by a client or server over the control channel to indicate
// it received a Ping.
type Pong struct {
}
