//go:build !windows && !darwin

package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
)

func totalRAMGB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if strings.HasPrefix(s.Text(), "MemTotal:") {
			kb, _ := strconv.Atoi(strings.Fields(s.Text())[1])
			return (kb + 512*1024) / (1024 * 1024)
		}
	}
	return 0
}

// Without a window: serve the page for a browser (development and testing).
func main() {
	root := flag.String("root", defaultRoot(), "launcher folder")
	addr := flag.String("addr", "127.0.0.1:8765", "listen address")
	setup := flag.Bool("setup", false, "show the installer page")
	flag.Parse()
	a := newApp(*root)
	if *setup {
		a = newSetupApp(*root)
	}
	url, err := serve(a, *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(url)
	select {}
}
