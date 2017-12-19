# proxy适用场景
ssh本地端口转发，解决 *【A可以主动连接B，B可以主动连接C的场景中，在A机器上间接连接C的需求】*

ssh远程端口转发，解决 *【B可以主动连接A/C的场景中，在主机B上搭桥让A间接连接C的需求】*

proxy解决 *【A/C都可以主动连接B的场景中，让A能够间接连接C的需求】*

## ngrok
对于proxy的需求，[ngrok](https://github.com/inconshreveable/ngrok)也可以实现，和[ngrok](https://github.com/inconshreveable/ngrok)相比，proxy做了以下修改

1. 增加横向扩展的能力
1. 增加bind到本地端口的能力，而不是类似[ngrok](https://github.com/inconshreveable/ngrok)访问一个公网的端口
1. 服务器对于所有的端口转发，只需要一个端口，这在防火墙限制比较严的场合比较管用。
1. 简化代码，删除http（s）的逻辑，只支持tcp
1. 证书做成可选，证书做加密、认证非常好的选择，不过并非必需

**部分基础代码复用[ngrok](https://github.com/inconshreveable/ngrok)，以后再优化**

# 部署运行
proxy包含三个程序
1. in 部署在内网A，用于发起方发起连接
2. out 部署在内网C，用于接收方接受连接
3. proxy 部署在A/C都可达的地方B

## Topic

参考[rabbitMQ](https://www.rabbitmq.com/)的[Topic](https://www.rabbitmq.com/tutorials/tutorial-five-python.html)语义。

在`out`注册到`server`时，会提供一个`Topic`（字符串），对于单个server，这个Topic必须`唯一`，并且后注册的同名Topic会`顶替`之前注册的out。

> 另外一个out使用和自己相同的Topic会将自己踢出来

而in发起到server的连接，也需要提供一个`Topic`，proxy根据Topic`路由`到对应的out。

## 运行
### in
``` bash
# 编译安装
go get github.com/qjw/proxy/in
# 运行
./in --config=./config.json
```
配置如下
``` json
{
	"networks": [
		{
			"server_host": "192.168.1.2",
			"server_port": 40001,
			"bind": "127.0.0.1",
			"port": 40004,
			"topic": "mysql"
		},
		{
			"server_host": "192.168.1.2",
			"server_port": 40001,
			"bind": "127.0.0.1",
			"port": 40002,
			"topic": "redis"
		}
	]
}
```

单个in实例可以同时转发多路topic，例如上面的配置将本地的40002端口绑定到server的topic`redis`，40004端口绑定到`mysql`。server可以相同也可以不一样

接下来我们像访问本地的方式访问远程mysql数据库
``` bash
mysql -u user -p -P 40004 -h 127.0.0.1
```

> in启动之后，并不会立即连接服务器proxy，而是有客户数据过来才会发起连接

### proxy

proxy比较简单，直接运行即可
``` bash
# 编译安装
go get github.com/qjw/proxy
# 运行
./proxy
```

> 为了提高响应效率，当out注册到proxy时，proxy会预先向out申请一些空闲的用于传输数据的TCP连接。

### nginx转发

在`/etc/nginx/nginx.conf`增加以下配置进行tcp转发。

> 不要在`/etc/nginx/conf.d/**`增加，这里的配置自动注入到http {}，nginx会报错

``` nginx
stream {
        server {
                listen 1234;
                proxy_pass 127.0.0.1:40001;
        }
}
```

进一步，`可以利用nginx做负载均衡`

### out
``` bash
# 编译安装
go get github.com/qjw/proxy/out
# 运行
./out --config=./config.json
```
``` json
{
	"networks": [
		{
			"server_host": "192.168.1.2",
			"server_port": 40001,
			"backend_host": "127.0.0.1",
			"backend_port": 22,
			"topic": "ssh"
		},
		{
			"server_host": "192.168.1.2",
			"server_port": 40001,
			"backend_host": "127.0.0.1",
			"backend_port": 3306,
			"topic": "mysql"
		}
	],
	"retry_interval": 2000,
	"heartbeat_interval": 2000,
	"heartbeat_timeout": 20000
}
```
和in类似，out单实例也支持多路topic。配置中和in不同的是，out并不绑定本地端口，而是增加了上游的主机/端口定义（类似[nginx upstream](http://nginx.org/en/docs/http/ngx_http_upstream_module.html)）。例如上面的配置就暴露了out所在主机的ssh/mysql到Topic（`ssh/mysql`）

> 和in类似，out启动之后，并不会立即连接上游upstream。而是有实际的数据达到才会发起连接

**out使用心跳报文做适当的稳定性检测，若因为任何原因掉线，out会一直重试，直到重新连接。**

## 横向扩展
对于proxy，利用nginx可以做负载均衡

对于out，若单点性能不够，可以开多个实例，每个实例连接一个proxy到上游的upstream。若单点够好，直接开一个实例，配置中指定不同主机的相同`Topic`即可。如下
``` json
{
	"networks": [
		{
			"server_host": "192.168.1.2",
			"server_port": 40001,
			"backend_host": "192.168.1.160",
			"backend_port": 3306,
			"topic": "mysql"
		},
		{
			"server_host": "192.168.1.3",
			"server_port": 40001,
			"backend_host": "127.0.0.1",
			"backend_port": 1080,
			"topic": "mysql"
		},
		{
			"server_host": "192.168.1.4",
			"server_port": 40001,
			"backend_host": "127.0.0.1",
			"backend_port": 1080,
			"topic": "mysql"
		}
	],
}
```

in作为客户端，通常不存在什么性能问题，可以随意部署多实例


# Sock5
作为proxy，自然就不能无视上网代理，我们可以在需要做上网跳板机上安装[ssocks](https://sourceforge.net/projects/ssocks)程序，这样就可以在本地1080（或其他端口）启用[socks5](https://zh.wikipedia.org/zh-cn/SOCKS#SOCKS5)代理。然后用out将1080端口暴露出去，并且使用in绑定到目标机器的localhost，再配合[SwitchyOmega](https://github.com/FelisCatus/SwitchyOmega/releases)插件就可以使用proxy隧道上网了。

源码<https://sourceforge.net/projects/ssocks>

``` bash
./configure
make
./src/ssocksd -b 127.0.0.1 --port 1080
# 或
./src/ssocksd -b 127.0.0.1 --port 1080 --daemon &
```

# todo
1. *配置
2. *日志
4. 认证和加密（可选）
8. 字段格式校验
11. *svr上游多重选择(自动重连/失败切换)
12. *type 替换测试确认
13. *等待的tunnel 如果对端挂了会出问题

3. 重新命名
5. *svr支持多dispatch
6. *svr支持自动重连
7. *文档 openssh、rdp、nginx
9. protobuf
10. *是否开启压缩