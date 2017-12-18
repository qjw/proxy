package utils

import (
	"fmt"
	"io"
	"net"
	"sync"
)

type Shutdowner interface {
	Shutdown()
}

func Join(c *net.TCPConn, c2 *net.TCPConn, s Shutdowner) (int64, int64) {
	var wait sync.WaitGroup

	pipe := func(to *net.TCPConn, from *net.TCPConn, bytesCopied *int64) {
		defer s.Shutdown()
		defer wait.Done()

		var err error
		*bytesCopied, err = io.Copy(to, from)
		if err != nil {
			fmt.Printf("Copied %d bytes before failing with error [%s]\n",
				*bytesCopied,
				err.Error())
		} else {
			// fmt.Printf("Copied %d bytes\n", *bytesCopied)
		}
		return
	}

	wait.Add(2)
	var fromBytes, toBytes int64
	go pipe(c, c2, &fromBytes)
	go pipe(c2, c, &toBytes)
	wait.Wait()
	return fromBytes, toBytes
}
