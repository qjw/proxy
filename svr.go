package main

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/qjw/proxy/msg"
	"github.com/qjw/proxy/utils"
)

type tunnel struct {
	r   *ControlRegistry
	m   *msg.OutRequest // 控制连接的请求包
	mng *net.TCPConn    // 控制连接

	proxyLock sync.Mutex
	proxies   map[*proxy]int // 已经正在工作的转发proxy

	frees chan *net.TCPConn  // 空闲数据链接
	out   chan (msg.Message) // 接收控制指令的ch

	shutdown     *utils.Shutdown // 关于tunnel自己的控制器
	loopShutdown *utils.Shutdown // 关于loop go routine的控制器
	readShutdown *utils.Shutdown // 关于read go routine的控制器
}

func (this tunnel) Type() string {
	return this.m.Type
}

func (this tunnel) RequestNewTunnel() {
	this.out <- &msg.NewDataRequest{
		Magic: utils.Magic(),
		Type:  this.m.Type,
	}
}

func (this *tunnel) GetFreeTunnel() (conn *net.TCPConn, err error) {
	var ok bool
	select {
	case conn, ok = <-this.frees:
		if !ok {
			err = fmt.Errorf("No proxy connections available, control is closing")
			return
		}
	default:
		fmt.Printf("No free tunnel in pool, requesting tunnel from control\n")
		this.RequestNewTunnel()

		select {
		case conn, ok = <-this.frees:
			if !ok {
				err = fmt.Errorf("No proxy connections available, control is closing")
				return
			}
		case <-time.After(time.Duration(utils.FreeTunnelTimeout) * time.Millisecond):
			err = fmt.Errorf("Timeout trying to get proxy connection")
			return
		}
	}
	fmt.Printf("get a empty data conn %p from [%s]\n", conn, this.m.Type)
	// 被弄走一个，提前再申请一个
	this.RequestNewTunnel()
	return
}

func (this *tunnel) RegisterDataConn(conn *net.TCPConn) error {
	select {
	case this.frees <- conn:
		fmt.Printf("registered data conn %p to [%s]\n", conn, this.m.Type)
		return nil
	default:
		fmt.Printf("frees buffer is full, discarding.")
		return fmt.Errorf("frees buffer is full, discarding.")
	}
}

func (this *tunnel) Add(s *proxy) {
	fmt.Printf("add proxy to tunnel %p\n", s)
	this.proxyLock.Lock()
	defer this.proxyLock.Unlock()

	if _, ok := this.proxies[s]; !ok {
		this.proxies[s] = 0
	} else {
		panic("repeat add to tunnel")
	}
}

func (this *tunnel) Del(s *proxy) {
	fmt.Printf("del proxy from tunnel %p\n", s)
	this.proxyLock.Lock()
	defer this.proxyLock.Unlock()

	if _, ok := this.proxies[s]; ok {
		delete(this.proxies, s)
	} else {
		panic("empty del from tunnel")
	}
}

func (this *tunnel) loop() {
	if this.mng == nil {
		panic("invalid tunnel")
	}
	defer this.loopShutdown.Complete()
	defer this.Shutdown()

	// write messages to the control channel
	for m := range this.out {
		if err := msg.WriteMsg(this.mng, m); err != nil {
			fmt.Println("write error ", err)
			break
		}
	}
}

func (this tunnel) read() {
	if this.mng == nil {
		panic("invalid tunnel")
	}
	defer this.readShutdown.Complete()
	defer this.Shutdown()

	for {
		_, _, err := msg.ReadMsg(this.mng)

		d, tp, err := msg.ReadMsg(this.mng)
		if err != nil {
			if err == io.EOF {
				fmt.Printf("tunnel %p read end\n", &this)
			} else {
				fmt.Println("Error to read message because of ", err)
			}
			break
		}
		if tp == "Ping" {
			// 自动回个心跳
			_, ok := d.(*msg.Ping)
			if !ok {
				fmt.Printf("invalid out resp type\n")
				break
			}
			msg.WriteMsg(this.mng, &msg.Pong{})
		}
	}
}

func (this tunnel) Shutdown() {
	this.shutdown.Begin()
}

func (this *tunnel) Run() {

	this.r.Add(this)
	defer this.r.Del(this)

	go this.loop()
	go this.read()

	// 事先请求一个data通道
	this.RequestNewTunnel()

	// 等待结束指令
	this.shutdown.WaitBegin()
	fmt.Printf("start to shutdown tunnel %p\n", this)

	// 关闭接收控制指令的ch
	close(this.out)

	// 关闭socket
	this.mng.CloseRead()
	this.mng.CloseWrite()
	this.mng.Close()

	// 关闭空闲的连接
	close(this.frees)
	for p := range this.frees {
		fmt.Printf("close free connection %p from tunnel %p\n", p, this)
		p.Close()
	}

	// 关闭关联的proxy
	this.proxyLock.Lock()
	for t, _ := range this.proxies {
		fmt.Printf("start to shutdown proxy %p from tunnel %p\n", t, this)
		t.Shutdown()
	}
	this.proxyLock.Unlock()

	// 等待go routine结束
	this.loopShutdown.WaitComplete()
	this.readShutdown.WaitComplete()
	this.shutdown.Complete()
}

////////////////////////////////////////////////////////////////////////////

type ControlRegistry struct {
	tunnels map[string]*tunnel
	sync.RWMutex
}

func NewControlRegistry() *ControlRegistry {
	return &ControlRegistry{
		tunnels: make(map[string]*tunnel),
	}
}

func (this *ControlRegistry) NewTunnel(mng *net.TCPConn, m *msg.OutRequest) {
	t := &tunnel{
		r:            this,
		mng:          mng,
		m:            m,
		proxies:      make(map[*proxy]int),
		frees:        make(chan *net.TCPConn, utils.TunnelBufLen),
		shutdown:     utils.NewShutdown(true),
		loopShutdown: utils.NewShutdown(false),
		readShutdown: utils.NewShutdown(false),
		out:          make(chan msg.Message),
	}
	go t.Run()
}

func (this *ControlRegistry) Add(s *tunnel) {
	fmt.Printf("add tunnel %p named [%s]\n", s, s.m.Type)
	this.Lock()
	defer this.Unlock()

	tp := s.Type()
	if t, ok := this.tunnels[tp]; ok {
		// 关闭旧的
		t.Shutdown()
	}
	this.tunnels[tp] = s
}

func (this *ControlRegistry) Del(s *tunnel) {
	fmt.Printf("del tunnel %p\n", s)
	this.Lock()
	defer this.Unlock()

	tp := s.Type()
	if v, ok := this.tunnels[tp]; ok {
		if v == s {
			fmt.Printf("delete tunnel %p from ControlRegistry\n", s)
			delete(this.tunnels, tp)
		} else {
			fmt.Printf("delete different tunnel todo(%p|%p)current ControlRegistry\n", s, v)
		}
	} else {
		panic("empty del\n")
	}
}

func (this *ControlRegistry) Get(tp string) *tunnel {
	this.RLock()
	defer this.RUnlock()
	return this.tunnels[tp]
}

func (this ControlRegistry) Shutdown() {
	fmt.Println("start to shutdown ControlRegistry")
	this.Lock()
	defer this.Unlock()

	// 尝试关闭
	for _, v := range this.tunnels {
		v.Shutdown()
	}

}

func (this ControlRegistry) WaitComplele() {
	for {
		time.Sleep(time.Millisecond * 100)
		this.Lock()

		if len(this.tunnels) == 0 {
			this.Unlock()
			break
		}

		this.Unlock()
	}
}

////////////////////////////////////////////////////////////////////////////

type proxy struct {
	r            *ProxyRegistry
	m            *msg.InRequest
	cli          *net.TCPConn
	svr          *net.TCPConn
	shutdown     *utils.Shutdown
	loopShutdown *utils.Shutdown
}

func (this *proxy) loop(c *ControlRegistry) {
	if this.cli == nil || this.svr != nil {
		panic("invalid proxy")
	}
	defer this.loopShutdown.Complete()
	defer this.cli.Close()
	defer this.Shutdown()

	// 回复报文
	initMsg := &msg.Response{
		Magic:   this.m.Magic,
		Request: "InRequest",
		Message: "",
	}

	if this.m.Version != utils.Version {
		initMsg.Message = fmt.Sprintf("invalid version [%s|%s]", this.m.Version, utils.Version)
		msg.WriteMsg(this.cli, initMsg)
		return
	}

	// 找到mng tunnel
	t := c.Get(this.m.Type)
	if t == nil {
		initMsg.Message = fmt.Sprintf("can not find tunnel [%s]", this.m.Type)
		msg.WriteMsg(this.cli, initMsg)
		return
	}
	t.Add(this)
	defer t.Del(this)

	// 获得空闲的连接
	newConn, err := t.GetFreeTunnel()
	if err != nil || newConn == nil {
		fmt.Println(err.Error())
		initMsg.Message = err.Error()
		msg.WriteMsg(this.cli, initMsg)
		return
	}
	this.svr = newConn
	defer this.svr.Close()

	// 上游服务器发送请求，用于激活data传输
	uniqKey := utils.Magic()
	msg.WriteMsg(newConn, &msg.DataActiveRequest{
		Magic: uniqKey,
		Type:  this.m.Type,
	})

	// 等待响应
	if err := msg.CheckResponse(this.svr, uniqKey, "DataActiveRequest"); err != nil {
		fmt.Println(err.Error())
		initMsg.Message = err.Error()
		msg.WriteMsg(this.cli, initMsg)
		return
	}

	msg.WriteMsg(this.cli, initMsg)
	fmt.Printf("ok,active proxy data exchange %p\n", this)

	// 开始数据交换
	utils.Join(this.cli, this.svr, this)
}

func (this proxy) Shutdown() {
	this.shutdown.Begin()
}

func (this *proxy) Run(c *ControlRegistry) {
	defer this.shutdown.Complete()

	this.r.Add(this)
	defer this.r.Del(this)

	go this.loop(c)
	// 连接服务器
	this.shutdown.WaitBegin()
	fmt.Printf("start to shutdown proxy %p\n", this)

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

////////////////////////////////////////////////////////////////////////////

type ProxyRegistry struct {
	proxies map[*proxy]int
	sync.Mutex
}

func NewProxyRegistry() *ProxyRegistry {
	return &ProxyRegistry{
		proxies: make(map[*proxy]int),
	}
}

func (this *ProxyRegistry) NewProxy(
	cli *net.TCPConn,
	m *msg.InRequest,
	c *ControlRegistry) {
	p := &proxy{
		r:            this,
		cli:          cli,
		m:            m,
		shutdown:     utils.NewShutdown(true),
		loopShutdown: utils.NewShutdown(false),
	}
	go p.Run(c)
}

func (this *ProxyRegistry) Add(s *proxy) {
	fmt.Printf("add proxy %p\n", s)
	this.Lock()
	defer this.Unlock()

	if _, ok := this.proxies[s]; !ok {
		this.proxies[s] = 0
	} else {
		panic("repeat add\n")
	}
}

func (this *ProxyRegistry) Del(s *proxy) {
	fmt.Printf("del proxy %p\n", s)
	this.Lock()
	defer this.Unlock()

	if _, ok := this.proxies[s]; ok {
		delete(this.proxies, s)
	} else {
		panic("empty del\n")
	}
}

func (this ProxyRegistry) Shutdown() {
	fmt.Println("start to shutdown ProxyRegistry")
	this.Lock()
	defer this.Unlock()

	// 尝试关闭
	for k, _ := range this.proxies {
		k.Shutdown()
	}

}

func (this ProxyRegistry) WaitComplele() {
	for {
		time.Sleep(time.Millisecond * 100)
		this.Lock()

		if len(this.proxies) == 0 {
			this.Unlock()
			break
		}

		this.Unlock()
	}
}

////////////////////////////////////////////////////////////////////////////
func handle(conn *net.TCPConn, p *ProxyRegistry, c *ControlRegistry) {
	m, tp, err := msg.ReadMsg(conn)
	if err != nil {
		fmt.Printf("read message error [%s]\n", err.Error())
		conn.Close()
		return
	}

	fmt.Printf("new request type [%s]\n", tp)
	if tp == "OutRequest" {
		req, ok := m.(*msg.OutRequest)
		if !ok {
			fmt.Print("invalid OutRequest type\n")
			return
		}

		initMsg := &msg.Response{
			Magic:   req.Magic,
			Request: tp,
			Message: "",
		}

		if req.Version != utils.Version {
			initMsg.Message = fmt.Sprintf("invalid version [%s|%s]", req.Version, utils.Version)
			msg.WriteMsg(conn, initMsg)
			conn.Close()
			return
		}

		// 校验合法性
		if req.Type == "" {
			initMsg.Message = "invalid type"
			msg.WriteMsg(conn, initMsg)
			conn.Close()
			return
		}

		msg.WriteMsg(conn, initMsg)

		c.NewTunnel(conn, req)
	} else if tp == "OutDataRequest" {
		req, ok := m.(*msg.OutDataRequest)
		if !ok {
			fmt.Print("invalid OutDataRequest type\n")
			return
		}

		initMsg := &msg.Response{
			Magic:   req.Magic,
			Request: tp,
			Message: "",
		}

		if req.Version != utils.Version {
			initMsg.Message = fmt.Sprintf("invalid version [%s|%s]", req.Version, utils.Version)
			msg.WriteMsg(conn, initMsg)
			conn.Close()
			return
		}

		// 找到mng tunnel
		t := c.Get(req.Type)
		if t == nil {
			initMsg.Message = fmt.Sprintf("can not find tunnel %s", req.Type)
			msg.WriteMsg(conn, initMsg)
			conn.Close()
			return
		}

		// 注册空闲的数据tunnel
		if err := t.RegisterDataConn(conn); err != nil {
			initMsg.Message = err.Error()
			msg.WriteMsg(conn, initMsg)
			conn.Close()
			return
		}

		msg.WriteMsg(conn, initMsg)
	} else if tp == "InRequest" {
		req, ok := m.(*msg.InRequest)
		if !ok {
			fmt.Print("invalid InRequest type\n")
			return
		}

		p.NewProxy(conn, req, c)
	} else {
		fmt.Printf("invalid tp %s\n", tp)
		msg.WriteMsg(conn, &msg.Response{
			Magic:   "invalid tp",
			Request: "invalid tp",
			Message: fmt.Sprintf("invalid type %s", tp),
		})
		conn.Close()

	}
}

func main() {
	fmt.Println("Starting the server ...")
	utils.RandomSeed()
	// tcp服务器
	listener, err := net.Listen("tcp", "0.0.0.0:40001")
	if err != nil {
		fmt.Println("Error listening", err.Error())
		return
	}

	p := NewProxyRegistry()
	c := NewControlRegistry()
	// 信号 和谐的退出
	exitMng := utils.NewExitManager()
	go exitMng.Run(func() {
		listener.Close()
		p.Shutdown()
		c.Shutdown()
	})

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
		go handle(tcpConn, p, c)
	}

	// 等待结束
	p.WaitComplele()
	c.WaitComplele()
}
