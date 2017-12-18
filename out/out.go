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

type connection struct {
	c            *connectionMng
	svr          *net.TCPConn
	cli          *net.TCPConn
	shutdown     *utils.Shutdown
	loopShutdown *utils.Shutdown
	network      *Network
}

func (this *connection) loop() {
	defer this.Shutdown()

	if this.cli == nil || this.svr != nil {
		panic("invalid connection")
	}
	defer this.loopShutdown.Complete()
	defer this.cli.Close()

	// 等待就绪
	m, tp, err := msg.ReadMsg(this.cli)
	if err != nil {
		fmt.Printf("read message error [%s]\n", err.Error())
		return
	}

	if tp != "DataActiveRequest" {
		fmt.Printf("invalid out resp type\n")
		return
	}

	req, ok := m.(*msg.DataActiveRequest)
	if !ok {
		fmt.Print("invalid OutRequest type\n")
		return
	}
	if req.Type != this.network.Topic {
		fmt.Printf("invalid Topic %s\n", req.Type)
		return
	}

	initMsg := &msg.Response{
		Magic:   req.Magic,
		Request: tp,
		Message: "",
	}

	// 收到请求之后，先连接服务器，确定之后再说
	svrConn, err := net.Dial(
		"tcp",
		fmt.Sprintf("%s:%d", this.network.BackendHost, this.network.BackendPort),
	)
	if err != nil {
		fmt.Println("Error connecting:", err)
		initMsg.Message = err.Error()
		msg.WriteMsg(this.cli, initMsg)
		return
	}
	defer svrConn.Close()
	tcpConn, _ := svrConn.(*net.TCPConn)
	this.svr = tcpConn

	// 回复
	msg.WriteMsg(this.cli, initMsg)

	// 开始数据交换
	utils.Join(this.cli, this.svr, this)
}

func (this connection) Shutdown() {
	this.shutdown.Begin()
}

func (this *connection) Run() {
	// 注册
	this.c.Add(this)
	defer this.c.Del(this)

	go this.loop()

	// 等待关闭
	this.shutdown.WaitBegin()
	fmt.Printf("start to shutdown connection %p\n", this)

	// 关闭socket
	this.cli.CloseRead()
	this.cli.CloseWrite()
	if this.svr != nil {
		this.svr.CloseRead()
		this.svr.CloseWrite()
	}

	// 等待loop结束
	this.loopShutdown.WaitComplete()
	// 成功结束
	this.shutdown.Complete()
}

///////////////////////////////////////////////////////////////////////////

type connectionMng struct {
	sessions map[*connection]int
	sync.Mutex
}

func NewControl() *connectionMng {
	return &connectionMng{
		sessions: make(map[*connection]int),
	}
}

func (this *connectionMng) NewConnection(cli *net.TCPConn, network *Network) {
	s := &connection{
		c:            this,
		cli:          cli,
		shutdown:     utils.NewShutdown(true),
		loopShutdown: utils.NewShutdown(false),
		network:      network,
	}
	go s.Run()
}

func (this *connectionMng) Add(s *connection) {
	fmt.Printf("add connection %p\n", s)
	this.Lock()
	defer this.Unlock()

	if _, ok := this.sessions[s]; !ok {
		this.sessions[s] = 0
	} else {
		panic("repeat add\n")
	}
}

func (this *connectionMng) Del(s *connection) {
	fmt.Printf("del connection %p\n", s)
	this.Lock()
	defer this.Unlock()

	if _, ok := this.sessions[s]; ok {
		delete(this.sessions, s)
	} else {
		panic("empty del\n")
	}
}

func (this connectionMng) Shutdown() {
	fmt.Printf("start to shutdown connectionMng\n")
	this.Lock()

	// 尝试关闭
	for k, _ := range this.sessions {
		k.Shutdown()
	}
	this.Unlock()
}

func (this connectionMng) WaitComplele() {
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

/////////////////////////////////////////////////////////////////////////////
type sessionGroup struct {
	c   *connectionMng
	mng *net.TCPConn

	shutdown          *utils.Shutdown
	heartbeatShutdown *utils.Shutdown
	beatCh            chan int
	lastPing          time.Time

	abortShutdown *utils.Shutdown
	breakFlag     bool
}

func NewSessionGroup() *sessionGroup {
	return &sessionGroup{
		abortShutdown: utils.NewShutdown(true),
		breakFlag:     false,
	}
}

func (this sessionGroup) Shutdown() {
	this.abortShutdown.Begin()
}

func (this sessionGroup) ShutdownAndRetry() {
	this.shutdown.Begin()
}

func (this *sessionGroup) Run(network *Network) {
	defer this.abortShutdown.Complete()

	go func() {
		this.abortShutdown.WaitBegin()
		this.breakFlag = true
		this.ShutdownAndRetry()
	}()

	for !this.breakFlag {
		conn, err := net.Dial(
			"tcp",
			fmt.Sprintf("%s:%d", network.ServerHost, network.ServerPort),
		)
		if err != nil {
			fmt.Println("Error connecting:", err)
			time.Sleep(time.Millisecond * time.Duration(gConfig.RetryInterval))
			continue
		}

		this.lastPing = time.Now()
		this.beatCh = make(chan int)
		this.heartbeatShutdown = utils.NewShutdown(false)
		this.shutdown = utils.NewShutdown(true)
		this.c = NewControl()
		tcpConn, _ := conn.(*net.TCPConn)
		this.mng = tcpConn

		go this.loop(network)
		go this.heartbeat()
		// 监控退出
		this.manager()

		if this.breakFlag {
			break
		}
		time.Sleep(time.Millisecond * time.Duration(gConfig.RetryInterval))
		fmt.Printf("retry connect in %p\n", this)
	}
}

func (this *sessionGroup) manager() {
	this.shutdown.WaitBegin()
	fmt.Printf("start to shutdown sessionGroup %p\n", this)

	this.mng.CloseRead()
	this.mng.CloseWrite()

	// 关闭数据连接
	this.c.Shutdown()
	// 等待结束
	this.c.WaitComplele()
	// 关闭心跳 （务必在c的后面）
	close(this.beatCh)
	// 等待心跳结束
	this.heartbeatShutdown.WaitComplete()

	this.mng.Close()
	// 回收
	this.shutdown.Complete()
}

func (this *sessionGroup) loop(network *Network) {
	defer this.ShutdownAndRetry()

	// 请求注册
	uniqKey := utils.Magic()
	initMsg := &msg.OutRequest{
		Magic:   uniqKey,
		Version: utils.Version,
		Type:    network.Topic,
	}
	msg.WriteMsg(this.mng, initMsg)

	// 确认回包
	if err := msg.CheckResponse(this.mng, uniqKey, "OutRequest"); err != nil {
		fmt.Println(err.Error())
		return
	}

	// 等待数据连接请求和心跳
	for {
		// 等待消息
		d, tp, err := msg.ReadMsg(this.mng)
		if err != nil {
			fmt.Printf("read message error [%s]\n", err.Error())
			break
		}
		if tp == "Pong" {
			_, ok := d.(*msg.Pong)
			if !ok {
				fmt.Printf("invalid out resp type\n")
				break
			}
			this.beatCh <- 1
			continue
		}

		resp, ok := d.(*msg.NewDataRequest)
		if !ok {
			fmt.Printf("invalid out resp type\n")
			break
		}

		if resp.Type != network.Topic {
			fmt.Printf("invalid Topic %s\n", resp.Type)
			continue
		}
		fmt.Printf("recv NewDataRequest\n")

		go func() {
			newConn, err := net.Dial(
				"tcp",
				fmt.Sprintf("%s:%d", network.ServerHost, network.ServerPort),
			)
			if err != nil {
				fmt.Println("Error connecting:", err)
				return
			}

			uniqKey := utils.Magic()
			initMsg := &msg.OutDataRequest{
				Magic:   uniqKey,
				Type:    network.Topic,
				Version: utils.Version,
			}
			msg.WriteMsg(newConn, initMsg)

			tcpConn, _ := newConn.(*net.TCPConn)
			if err := msg.CheckResponse(tcpConn, uniqKey, "OutDataRequest"); err != nil {
				fmt.Println(err.Error())
				tcpConn.Close()
				return
			}

			this.c.NewConnection(tcpConn, network)
		}()
	}
}

func (this *sessionGroup) heartbeat() {
	defer this.ShutdownAndRetry()
	defer this.heartbeatShutdown.Complete()

	flag := false
	for {
		if flag {
			break
		}

		select {
		case _, ok := <-this.beatCh:
			// 收到心跳回报
			if !ok {
				flag = true
			}
			this.lastPing = time.Now()
			break
		case <-time.After(time.Millisecond * time.Duration(gConfig.HeartbeatInterval)):
			// 检查心跳
			if time.Since(this.lastPing) > time.Millisecond*time.Duration(gConfig.HeartbeatTimeout) {
				fmt.Println("Lost heartbeat")
				flag = true
			}

			// 发送心跳
			msg.WriteMsg(this.mng, &msg.Ping{})
			break
		}

	}
}

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

	// 信号 和谐的退出
	sessions := make([]*sessionGroup, 0)
	exitMng := utils.NewExitManager()
	go exitMng.Run(func() {
		for _, v := range sessions {
			v.Shutdown()
		}
	})

	var wait sync.WaitGroup
	wait.Add(len(gConfig.Networks))
	for _, v := range gConfig.Networks {
		s := NewSessionGroup()
		network := v
		sessions = append(sessions, s)
		go func() {
			defer wait.Done()
			s.Run(network)
		}()
	}
	wait.Wait()
}
