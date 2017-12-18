package utils

import (
	"os"
	"os/signal"
	"syscall"
)

type breakMng struct {
	done chan bool
}

func NewExitManager() *breakMng {
	mng := &breakMng{}
	return mng
}

func (this breakMng) Wait() {
	if this.done == nil {
		panic("invalid chan")
	}

	defer close(this.done)

	<-this.done
}

func (this *breakMng) Run(handle func()) {
	// 信号
	sigs := make(chan os.Signal, 1)
	defer close(sigs)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

	this.done = make(chan bool)
	defer func() {
		this.done <- true
	}()

	<-sigs
	handle()
}
