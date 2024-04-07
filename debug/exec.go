package main

import (
	"bufio"
	"context"
	"fmt"
	"os/exec"
)

func main() {
	println("Hello, World!")
	cmdCtx, cmdDone := context.WithCancel(context.Background())

	command := []string{"nginx"}

	//Split command to slice
	name, args := command[0], command[1:]

	cmd := exec.Command(name, args...)

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	scanner := bufio.NewScanner(stdout)
	scannererr := bufio.NewScanner(stderr)

	// Run and wait for Cmd to return, discard Status
	err := cmd.Start()
	println("start")
	if err != nil {
		println("error")
		cmdDone()
	}
	//read stdout and stderr
	go func() {
		for scanner.Scan() {
			fmt.Println(scanner.Text())
		}
	}()
	go func() {
		for scannererr.Scan() {
			fmt.Println(scannererr.Text())
		}
	}()

	println("wait")
	go func() {
		_ = cmd.Wait()
		println("wait done")
		cmdDone()
	}()

	println("done")
	<-cmdCtx.Done()
	println("exit " + string(cmd.ProcessState.ExitCode()))
}
