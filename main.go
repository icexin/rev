package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/hashicorp/yamux"
	"github.com/spf13/pflag"
)

var (
	serverAddr = pflag.StringP("addr", "a", ":8421", "listen(server)|connect(client) address")
	proxyPorts = pflag.StringArrayP("port", "p", []string{}, "proxy ports")
	asServer   = pflag.BoolP("server", "s", false, "server mode")
)

func ProxyConn(client, server net.Conn) {
	wg := new(sync.WaitGroup)
	wg.Add(2)
	go func() {
		defer wg.Done()
		io.Copy(client, server)
		client.Close()
	}()
	go func() {
		defer wg.Done()
		io.Copy(server, client)
		server.Close()
	}()
	wg.Wait()
}

type server struct {
}

func (s *server) ServeConn(conn net.Conn) {
	defer conn.Close()
	var bindport uint32
	err := binary.Read(conn, binary.BigEndian, &bindport)
	if err != nil {
		log.Printf("read port:%s", err)
		return
	}
	log.Printf("accept conn %s on %d", conn.RemoteAddr(), bindport)

	l, err := net.Listen("tcp", ":"+strconv.Itoa(int(bindport)))
	if err != nil {
		log.Print(err)
		return
	}
	defer func() {
		log.Printf("listen %d closed", bindport)
		l.Close()
	}()

	sess, err := yamux.Client(conn, nil)
	if err != nil {
		log.Print(err)
		return
	}
	defer sess.Close()

	go func() {
		<-sess.CloseChan()
		l.Close()
	}()

	for {
		clientConn, err := l.Accept()
		if err != nil {
			log.Print(err)
			return
		}

		serverConn, err := sess.Open()
		if err != nil {
			log.Printf("open remote session:%s", err)
			return
		}
		go ProxyConn(clientConn, serverConn)
	}
}

func runserver() {
	s := new(server)
	l, err := net.Listen("tcp", *serverAddr)
	if err != nil {
		log.Fatal(err)
	}
	defer l.Close()
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		go s.ServeConn(conn)
	}
}

type client struct {
}

func (c *client) reconnect(addr string) net.Conn {
	n := 1
	for {
		conn, err := net.Dial("tcp", addr)
		if err == nil {
			return conn
		}
		n = n * 2
		if n >= 30 {
			n = 30
		}
		time.Sleep(time.Duration(n) * time.Second)
	}
}

func (c *client) servePortOnce(serverAddr string, localPort, remotePort uint32) error {
	conn := c.reconnect(serverAddr)
	defer conn.Close()
	log.Printf("connect %s for port %d:%d", serverAddr, localPort, remotePort)
	err := binary.Write(conn, binary.BigEndian, remotePort)
	if err != nil {
		return err
	}
	sess, err := yamux.Server(conn, nil)
	if err != nil {
		return err
	}
	defer sess.Close()

	for {
		clientConn, err := sess.Accept()
		if err != nil {
			return err
		}
		serverConn, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", localPort))
		if err != nil {
			log.Print(err)
			clientConn.Close()
			continue
		}
		go ProxyConn(clientConn, serverConn)
	}
}

func (c *client) ServePort(serverAddr string, localPort, remotePort uint32) {
	for {
		err := c.servePortOnce(serverAddr, localPort, remotePort)
		if err != nil {
			log.Print(err)
		}
		time.Sleep(time.Second)
	}
}

func runclient() {
	c := new(client)
	for _, portstr := range *proxyPorts {
		var localPort, remotePort uint32
		n, err := fmt.Sscanf(portstr, "%d:%d", &localPort, &remotePort)
		if err != nil || n != 2 {
			log.Fatalf("bad ports:%s", portstr)
		}
		go c.ServePort(*serverAddr, localPort, remotePort)
	}
	select {}
}

func main() {
	pflag.Parse()
	if *asServer {
		runserver()
	} else {
		runclient()
	}
}
