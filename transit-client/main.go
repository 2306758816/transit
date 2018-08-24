package main

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// buffSize 缓存大小
const buffSize = 1024

func main() {
	raddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:8000")
	laddr, _ := net.ResolveTCPAddr("tcp", ":8001")
	conn, err := net.DialTCP("tcp", laddr, raddr)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer conn.Close()

	for {
		fmt.Printf("Start Read %s Data.\n", raddr.String())
		// 接收请求数据
		var b [buffSize]byte
		n, err := conn.Read(b[:])
		if err != nil {
			if err == io.EOF {
				fmt.Println("Server Close")
				return
			}
			fmt.Println(err)
			continue
		}
		// 拨号HTTP服务
		rconn, err := net.Dial("tcp", ":9999")
		if err != nil {
			fmt.Println(err)
			return
		}
		// defer rconn.Close()
		// 处理请求及响应
		var wg sync.WaitGroup
		wg.Add(2)
		// 处理请求
		go func() {
			defer wg.Done()
			for {
				_, err := rconn.Write(b[:n])
				if err != nil {
					fmt.Println(err)
					return
				}

				if n < buffSize {
					break
				}
				n, err = conn.Read(b[:])
				if err != nil {
					fmt.Println(err)
					return
				}
			}
		}()
		// 处理响应
		go func() {
			defer wg.Done()
			var rb [buffSize]byte
			var n int
			var err error

			datach := make(chan error, 1)
			for {
				go func() {
					n, err = rconn.Read(rb[:])
					datach <- err
					if err != nil {
						return
					}
				}()
				select {
				case <-time.After(time.Second):
					fmt.Println("done")
					return
				case err = <-datach:
					if err != nil {
						if err == io.EOF {
							fmt.Println(err)
						}
						return
					}
					_, err = conn.Write(rb[:n])
					if err != nil {
						fmt.Println(err)
						return
					}
				}
			}
		}()
		wg.Wait()
		// 关闭HTTP服务连接
		rconn.Close()
	}
}
