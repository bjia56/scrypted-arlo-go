package scrypted_arlo_go

// adapted from https://github.com/bluenviron/gortsplib/blob/93b02bc0e851df641bc20a2f488bffccad5994ed/examples/proxy/server.go

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"

	"github.com/bluenviron/gortsplib/v4"
	"github.com/bluenviron/gortsplib/v4/pkg/base"
	"github.com/bluenviron/gortsplib/v4/pkg/description"
	"github.com/bluenviron/gortsplib/v4/pkg/format"
	"github.com/pion/rtp"
)

type RTSPServer struct {
	server *gortsplib.Server
	logger *TCPLogger

	stream *gortsplib.ServerStream
	mutex  sync.Mutex

	audioListener net.Conn
	videoListener net.Conn

	AudioPort int
	VideoPort int

	started <-chan struct{}
}

func NewRTSPServer(loggerPort int, rtspPort int) (*RTSPServer, error) {
	logger, err := NewTCPLogger(loggerPort, "RTSPServer")
	if err != nil {
		return nil, err
	}

	r := &RTSPServer{logger: logger}
	r.server = &gortsplib.Server{
		Handler:                  r,
		RTSPAddress:              fmt.Sprintf("localhost:%d", rtspPort),
		DisableRTCPSenderReports: true,
	}

	return r, nil
}

func (r *RTSPServer) Printf(msg string, args ...any) {
	r.logger.Send(fmt.Sprintf(msg, args...))
}

func (r *RTSPServer) Println(msg string, args ...any) {
	r.Printf(msg+"\n", args...)
}

func (r *RTSPServer) Start() error {
	var err error

	started := make(chan struct{})
	r.started = started

	desc := &description.Session{}
	var (
		audioMedia *description.Media
		videoMedia *description.Media
	)

	audioMedia, r.audioListener, r.AudioPort, err = r.initializeOpusRTPListener()
	if err != nil {
		return fmt.Errorf("failed to initialize audio listener: %w", err)
	}

	videoMedia, r.videoListener, r.VideoPort, err = r.initializeH264RTPListener()
	if err != nil {
		return fmt.Errorf("failed to initialize video listener: %w", err)
	}

	r.mutex.Lock()
	defer r.mutex.Unlock()

	desc.Medias = append(desc.Medias, audioMedia, videoMedia)
	r.stream = gortsplib.NewServerStream(r.server, desc)

	if err = r.server.Start(); err != nil {
		return fmt.Errorf("could not start server: %w", err)
	}

	close(started)
	r.Println("RTSPServer started")
	return nil
}

func (r *RTSPServer) initializeRTPListener(medi *description.Media) (conn net.Conn, port int, err error) {
	// cleanup in case of error
	defer func() {
		if err != nil && conn != nil {
			conn.Close()
			conn = nil
		}
	}()

	conn, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return conn, 0, err
	}

	listenerType := fmt.Sprintf("%s:%s", medi.Type, medi.Formats[0].Codec())

	go func() {
		inboundRTPPacket := make([]byte, UDP_PACKET_SIZE)
		<-r.started
		for {
			n, _, err := conn.(*net.UDPConn).ReadFrom(inboundRTPPacket)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					r.Println("Error during %s track read: %s", listenerType, err)
				}
				return
			}

			var pkt rtp.Packet
			if err = pkt.Unmarshal(inboundRTPPacket[:n]); err != nil {
				r.Println("Error unmarshaling RTP packet: %s", err)
				continue
			}

			if err = r.stream.WritePacketRTP(medi, &pkt); err != nil {
				if !errors.Is(err, io.ErrClosedPipe) {
					r.Println("Error writing to %s track: %s", listenerType, err)
				}
				return
			}
		}
	}()

	port, err = strconv.Atoi(strings.Split(conn.LocalAddr().String(), ":")[1])
	if err != nil {
		return conn, 0, err
	}

	r.Println("Created %s RTP listener at udp://127.0.0.1:%d", listenerType, port)
	return conn, port, nil
}

func (r *RTSPServer) initializeOpusRTPListener() (medi *description.Media, conn net.Conn, port int, err error) {
	medi = &description.Media{
		Type:    description.MediaTypeAudio,
		Formats: []format.Format{&format.Opus{}},
	}
	conn, port, err = r.initializeRTPListener(medi)
	return
}

func (r *RTSPServer) initializeH264RTPListener() (medi *description.Media, conn net.Conn, port int, err error) {
	medi = &description.Media{
		Type:    description.MediaTypeVideo,
		Formats: []format.Format{&format.H264{}},
	}
	conn, port, err = r.initializeRTPListener(medi)
	return
}

func (r *RTSPServer) OnConnOpen(ctx *gortsplib.ServerHandlerOnConnOpenCtx) {
	r.Println("conn opened")
}

func (r *RTSPServer) OnConnClose(ctx *gortsplib.ServerHandlerOnConnCloseCtx) {
	r.Println("conn closed (%v)", ctx.Error)
}

func (r *RTSPServer) OnSessionOpen(ctx *gortsplib.ServerHandlerOnSessionOpenCtx) {
	r.Println("session opened")
}

func (r *RTSPServer) OnSessionClose(ctx *gortsplib.ServerHandlerOnSessionCloseCtx) {
	r.Println("session closed")
}

func (r *RTSPServer) OnDescribe(ctx *gortsplib.ServerHandlerOnDescribeCtx) (*base.Response, *gortsplib.ServerStream, error) {
	r.Println("describe request")

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// stream is not available yet
	if r.stream == nil {
		return &base.Response{
			StatusCode: base.StatusNotFound,
		}, nil, nil
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, r.stream, nil
}

func (r *RTSPServer) OnSetup(ctx *gortsplib.ServerHandlerOnSetupCtx) (*base.Response, *gortsplib.ServerStream, error) {
	r.Println("setup request")

	r.mutex.Lock()
	defer r.mutex.Unlock()

	// stream is not available yet
	if r.stream == nil {
		return &base.Response{
			StatusCode: base.StatusNotFound,
		}, nil, nil
	}

	return &base.Response{
		StatusCode: base.StatusOK,
	}, r.stream, nil
}

func (r *RTSPServer) OnPlay(ctx *gortsplib.ServerHandlerOnPlayCtx) (*base.Response, error) {
	r.Println("play request")

	return &base.Response{
		StatusCode: base.StatusOK,
	}, nil
}

func (r *RTSPServer) Close() {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if r.stream != nil {
		r.stream.Close()
		r.stream = nil
	}
	if r.server != nil {
		r.server.Close()
		r.server = nil
	}
	if r.audioListener != nil {
		r.audioListener.Close()
		r.audioListener = nil
	}
	if r.videoListener != nil {
		r.videoListener.Close()
		r.videoListener = nil
	}
}
