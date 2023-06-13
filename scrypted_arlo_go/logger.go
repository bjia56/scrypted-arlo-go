package scrypted_arlo_go

import (
	"fmt"
	"net"
)

type TCPLogger struct {
	conn net.Conn
}

func NewTCPLogger(loggerPort int, name string) (*TCPLogger, error) {
	logger, err := net.Dial("tcp4", fmt.Sprintf("localhost:%d", loggerPort))
	if err != nil {
		return nil, fmt.Errorf("could not connect to logger server: %w", err)
	}
	t := &TCPLogger{conn: logger}
	t.Send(fmt.Sprintf("%s connected to logging server localhost:%d\n", name, loggerPort))
	return t, nil
}

func (t *TCPLogger) Send(s string) {
	if t.conn != nil {
		t.conn.Write([]byte(s))
	}
}

func (t *TCPLogger) Close() {
	if t.conn != nil {
		t.conn.Close()
		t.conn = nil
	}
}
