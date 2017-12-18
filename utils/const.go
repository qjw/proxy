package utils

const (
	DftType           string = "default" // 默认的Type
	Version           string = "0.0.1"   // 版本号
	FreeTunnelTimeout int    = 5000      // 获取空闲tunnel的超时(毫秒）
	TunnelBufLen      int    = 100       // 空闲tunnel 缓冲区长度
	MagicLen          int    = 16        // magic的字符串长度
	RetryInterval     int    = 2000      // 重连间隔(毫秒）
	HeartbeatInterval int    = 2000      // 心跳检测间隔(毫秒）
	HeartbeatTimeout  int    = 20 * 1000 // 心跳超时(毫秒）
)
