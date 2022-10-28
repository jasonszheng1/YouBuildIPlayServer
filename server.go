package main

import (
    "fmt"
    "net"
)

func main() {
	listen, err := net.ListenUDP("udp", &net.UDPAddr{
		IP: net.IPv4(127, 0, 0, 1),
		Port: 30000,
		})
	if err != nil {
		fmt.Println("listen faild, err:", err)
		return
	}
	defer listen.Close()

	for {
		var data [1024]byte
		fmt.Println("reading")
		n, addr, err := listen.ReadFromUDP(data[:])
		if err != nil {
			fmt.Println("read error:", err)
			continue
		}
		fmt.Printf("data:%v addr:%v count:%v\n", string(data[:n]), addr, n)
	}
}

