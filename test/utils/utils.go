package test_utils

import (
	"errors"
	"net"
	"time"
)

func ConnectionTest(testAddress string, checkOnline bool) error {
	doneConnecting := make(chan error)

	// try to connect to TestAddress to see if the server is up, if yes end the test
	go func() {
		timeout := time.After(20 * time.Second)
		tick := time.Tick(1 * time.Second)
		now := time.Now()
		for {
			select {
			case <-timeout:
				doneConnecting <- errors.New("timeout Connecting")
				return
			case <-tick:
				conn, err := net.DialTimeout("tcp", testAddress, 1*time.Second)
				//TODO: UDP support, when we need it
				if err == nil {
					conn.Close()
					if checkOnline {
						println("Connected to server after", time.Since(now).String())
						doneConnecting <- nil
						return
					}
				} else {
					if !checkOnline {
						println("Server is offline after", time.Since(now).String())
						doneConnecting <- nil
						return
					}
				}
			}
		}
	}()

	return <-doneConnecting
}
