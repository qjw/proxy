package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/qjw/proxy/msg"
	"github.com/qjw/proxy/utils"
)

var (
	gConfig *Config = nil
)

type session struct {
	c            *control
	svr          *net.TCPConn
	cli          *net.TCPConn
	shutdown     *utils.Shutdown
	loopShutdown *utils.Shutdown
	network      *Network
}

func (this *session) loop() {
	defer this.Shutdown()

	if this.cli == nil || this.svr != nil {
		panic("invalid session")
	}
	defer this.loopShutdown.Complete()
	defer this.cli.Close()

	// 收到请求之后，先连接服务器，确定之后再说
	svrConn, err := net.Dial(
		"tcp",
		fmt.Sprintf("%s:%d", this.network.ServerHost, this.network.ServerPort),
	)
	if err != nil {
		fmt.Println("Error connecting:", err)
		return
	}
	defer svrConn.Close()
	tcpConn, _ := svrConn.(*net.TCPConn)
	this.svr = tcpConn

	// 服务器发送请求
	uniqKey := utils.Magic()
	initMsg := &msg.InRequest{
		Magic:   uniqKey,
		Version: utils.Version,
		Type:    this.network.Topic,
	}
	msg.WriteMsg(tcpConn, initMsg)

	// 等待响应
	if err := msg.CheckResponse(tcpConn, uniqKey, "InRequest"); err != nil {
		fmt.Println(err.Error())
		return
	}
	fmt.Printf("ok,start session data exchange %p\n", this)

	// 开始数据交换
	utils.Join(this.cli, this.svr, this)

}

func (this session) Shutdown() {
	this.shutdown.Begin()
}

func (this *session) Run() {
	// 成功结束
	defer this.shutdown.Complete()

	this.c.Add(this)
	defer this.c.Del(this)

	go this.loop()
	// 连接服务器
	this.shutdown.WaitBegin()
	fmt.Printf("start to shutdown session %p\n", this)

	// 关闭socket
	this.cli.CloseRead()
	this.cli.CloseWrite()
	if this.svr != nil {
		this.svr.CloseRead()
		this.svr.CloseWrite()
	}

	// 等待loop结束
	this.loopShutdown.WaitComplete()
}

///////////////////////////////////////////////////////////////////////////

type control struct {
	sessions map[*session]int
	sync.Mutex
}

func NewControl() *control {
	return &control{
		sessions: make(map[*session]int),
	}
}

func (this *control) NewSession(cli *net.TCPConn, network *Network) {
	s := &session{
		c:            this,
		cli:          cli,
		shutdown:     utils.NewShutdown(true),
		loopShutdown: utils.NewShutdown(false),
		network:      network,
	}
	go s.Run()
}

func (this *control) Add(s *session) {
	fmt.Printf("add session %p\n", s)
	this.Lock()
	defer this.Unlock()

	if _, ok := this.sessions[s]; !ok {
		this.sessions[s] = 0
	} else {
		panic("repeat add\n")
	}
}

func (this *control) Del(s *session) {
	fmt.Printf("del session %p\n", s)
	this.Lock()
	defer this.Unlock()

	if _, ok := this.sessions[s]; ok {
		delete(this.sessions, s)
	} else {
		panic("empty del\n")
	}
}

func (this control) Shutdown() {
	fmt.Println("start to shutdown control\n")
	this.Lock()

	// 尝试关闭
	for k, _ := range this.sessions {
		k.Shutdown()
	}
	this.Unlock()
}

func (this control) WaitComplele() {
	for {
		time.Sleep(time.Millisecond * 100)
		this.Lock()

		if len(this.sessions) == 0 {
			this.Unlock()
			break
		}

		this.Unlock()
	}
}

///////////////////////////////////////////////////////////////////////////
func checkConfig() {
	if jsonBytes, err := json.MarshalIndent(gConfig, "", "    "); err != nil {
		panic(err)
	} else {
		fmt.Println(string(jsonBytes))
	}
	if len(gConfig.Networks) < 1 || len(gConfig.Networks) > 16 {
		panic("invalid network config,network count in(1,16")
	}

	// 不同host/port 的subject可以相同
	//	topics := make(map[string]int)
	//	for _, v := range gConfig.Networks {
	//		topics[v.Topic] = 0
	//	}
	//	if len(topics) != len(gConfig.Networks) {
	//		panic("topic can not duplicate")
	//	}
}

func main() {
	confPath := flag.String("config", "", "配置文件")
	flag.Parse()
	if confPath == nil || *confPath == "" {
		gConfig = defaultConfig()
	} else {
		gConfig = parseConfig(*confPath)
	}
	checkConfig()

	fmt.Println("Starting the server ...")
	utils.RandomSeed()

	listenrs := make(map[net.Listener]*Network)
	c := NewControl()

	// 信号 和谐的退出
	exitMng := utils.NewExitManager()
	go exitMng.Run(func() {
		for k, _ := range listenrs {
			k.Close()
		}
		c.Shutdown()
	})

	for _, v := range gConfig.Networks {
		network := v

		// tcp服务器
		listener, err := net.Listen(
			"tcp",
			fmt.Sprintf("%s:%d", network.Bind, network.Port),
		)
		if err != nil {
			fmt.Println("Error listening", err.Error())
			return
		}
		listenrs[listener] = network
	}

	var wait sync.WaitGroup
	wait.Add(len(gConfig.Networks))
	for k, v := range listenrs {
		listener := k
		network := v
		go func() {
			defer wait.Done()
			for {
				conn, err := listener.Accept()
				if conn == nil {
					fmt.Println("listener accept ended")
					break
				}
				if err != nil {
					fmt.Println("Error accepting", err.Error())
					break
				}

				tcpConn, _ := conn.(*net.TCPConn)
				c.NewSession(tcpConn, network)
			}
		}()
	}

	// 等待结束
	wait.Wait()
	c.WaitComplele()
}
