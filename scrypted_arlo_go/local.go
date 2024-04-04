package scrypted_arlo_go

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/beatgammit/rtsp"
)

type LocalStreamProxy struct {
	infoLogger  *TCPLogger
	debugLogger *TCPLogger

	basestationHostname string
	basestationIP       string
	certPEM             string
	keyPEM              string

	tlsConfig    *tls.Config
	listener     net.Listener
	listenerPort int
	backend      net.Conn
}

func NewLocalStreamProxy(
	infoLoggerPort, debugLoggerPort int,
	basestationHostname string,
	basestationIP string,
	certPEM string,
	keyPEM string,
) (*LocalStreamProxy, error) {
	name := "LocalStreamProxy"
	infoLogger, err := NewTCPLogger(infoLoggerPort, name)
	if err != nil {
		return nil, err
	}
	debugLogger, err := NewTCPLogger(debugLoggerPort, name)
	if err != nil {
		return nil, err
	}

	cert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		return nil, fmt.Errorf("could not load TLS certificate and key: %w", err)
	}

	return &LocalStreamProxy{
		infoLogger:          infoLogger,
		debugLogger:         debugLogger,
		basestationHostname: basestationHostname,
		basestationIP:       basestationIP,
		certPEM:             certPEM,
		keyPEM:              keyPEM,
		tlsConfig: &tls.Config{
			Certificates:       []tls.Certificate{cert},
			InsecureSkipVerify: true,
		},
	}, nil
}

func (l *LocalStreamProxy) Info(msg string, args ...any) {
	l.infoLogger.Send(fmt.Sprintf(msg+"\n", args...))
}

func (l *LocalStreamProxy) Debug(msg string, args ...any) {
	l.debugLogger.Send(fmt.Sprintf(msg+"\n", args...))
}

func (l *LocalStreamProxy) Start() (port int, err error) {
	// Create TCP listener
	l.listener, err = net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("error creating TCP listener: %w", err)
	}

	l.Info("TCP proxy server listening on %s", l.listener.Addr())

	port, err = strconv.Atoi(strings.Split(l.listener.Addr().String(), ":")[1])
	if err != nil {
		l.listener.Close()
		return 0, fmt.Errorf("could not get port number: %s", err)
	}

	l.listenerPort = port

	// Accept incoming connections and handle them in a new goroutine
	go func() {
		defer l.listener.Close()

		clientConn, err := l.listener.Accept()
		if err != nil {
			l.Info("Error accepting connection: %s", err)
			return
		}

		l.handleClient(clientConn)
	}()

	return port, nil
}

const (
	cBufferLen = 4096
	sBufferLen = 40960
)

func (l *LocalStreamProxy) handleClient(clientConn net.Conn) {
	defer clientConn.Close()

	// Connect to the backend server
	backendConn, err := tls.Dial("tcp", fmt.Sprintf("%s:554", l.basestationIP), l.tlsConfig)
	if err != nil {
		l.Info("Failed to connect to the backend server: %s", err)
		return
	}
	l.backend = backendConn
	defer backendConn.Close()

	l.Info("Proxying from %s to %s", clientConn.RemoteAddr(), backendConn.RemoteAddr())

	cBuffer := make([]byte, cBufferLen)
	sBuffer := make([]byte, sBufferLen)
	nonce := 0

	go func() {
		defer backendConn.Close()
		defer clientConn.Close()
		for {
			// Read data from the server
			n, err := backendConn.Read(sBuffer)
			if err != nil {
				l.Info("Error reading from server: %s", err)
				break
			}

			if n == sBufferLen {
				l.Info("Warning: local stream buffer may be too small")
			}

			rr, err := rtsp.ReadResponse(bytes.NewBuffer(sBuffer))
			if err != nil {
				if err == rtsp.NOT_RTSP_PACKET {
					_, err = clientConn.Write(sBuffer[:n])
					if err != nil {
						l.Info("Error writing to client: %s", err)
						break
					}
					continue
				}
				l.Info("Error parsing rtsp response: %s", err)
				break
			}

			if rr.Header.Get("Nonce") != "" {
				nonce, err = strconv.Atoi(rr.Header.Get("Nonce"))
				if err != nil {
					l.Info("Error parsing nonce: %s", err)
					break
				}
			}

			s := rr.String()
			s = strings.ReplaceAll(s, "Cseq:", "CSeq:")
			s = strings.ReplaceAll(s, "Rtp-Info:", "RTP-Info:")
			l.Debug("Incoming:\n%s", s)

			// Forward the data to the client
			_, err = clientConn.Write([]byte(s))
			if err != nil {
				l.Info("Error writing to client: %s", err)
				break
			}
		}
	}()

	for {
		// Read data from the client
		_, err := clientConn.Read(cBuffer)
		if err != nil {
			l.Info("Error reading from client: %s", err)
			break
		}

		rr, err := rtsp.ReadRequest(bytes.NewBuffer(cBuffer))
		if err != nil {
			l.Info("Error parsing rtsp request: %s", err)
			break
		}

		if nonce != 0 {
			nonce += 1
			rr.Header.Add("Nonce", fmt.Sprintf("%d", nonce))
		}

		s := rr.String()
		s = strings.ReplaceAll(s, fmt.Sprintf("rtsp://localhost:%d", l.listenerPort), fmt.Sprintf("rtsp://%s", l.basestationHostname))
		s = strings.ReplaceAll(s, "Cseq:", "CSeq:")
		l.Debug("Outgoing:\n%s", s)

		// Forward the data to the backend
		_, err = backendConn.Write([]byte(s))
		if err != nil {
			l.Info("Error writing to backend: %s", err)
			break
		}
	}
}

func (l *LocalStreamProxy) Close() {
	if l.listener != nil {
		l.listener.Close()
	}
	if l.backend != nil {
		l.backend.Close()
	}
}
