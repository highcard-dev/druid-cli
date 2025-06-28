package test_utils

import (
	"fmt"
	"net"
	"time"
)

var NoTcpTester = func(answer string, port int) error {

	time.Sleep(1000 * time.Millisecond) // Simulate some delay

	err := TcpTester(answer, port)
	if err != nil {
		return nil
	}
	return fmt.Errorf("tcpTester should not have been called, but it was")
}

var TcpTester = func(answer string, port int) error {
	println("dial tcpTester")
	//tcp connect to 12349 and send test data
	con, err := net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return fmt.Errorf("failed to dial tcp: %v", err)
	}
	defer con.Close()

	println("write tcpTester")
	_, err = con.Write([]byte("test"))
	if err != nil {
		return fmt.Errorf("failed to write test data: %v", err)
	}

	if answer == "" {
		return nil
	}

	println("read tcpTester")
	data := make([]byte, 1024)
	n, err := con.Read(data)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	dataStr := string(data[:n])
	if dataStr != answer {
		return fmt.Errorf("unexpected response: %s != %s ", dataStr, answer)
	}
	return nil
}

var UdpTester = func(answer string, port int) error {
	//udp connect to 12349 and send test data
	con, err := net.DialUDP("udp", nil, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port})
	if err != nil {
		return fmt.Errorf("failed to dial udp: %v", err)
	}
	defer con.Close()

	_, err = con.Write([]byte("test"))
	if err != nil {
		return fmt.Errorf("failed to write test data: %v", err)
	}

	if answer == "" {
		return nil
	}

	data := make([]byte, 1024)
	n, _, err := con.ReadFromUDP(data)
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	dataStr := string(data[:n])
	if dataStr != answer {
		return fmt.Errorf("unexpected response: %s != %s ", dataStr, answer)
	}
	return nil
}
