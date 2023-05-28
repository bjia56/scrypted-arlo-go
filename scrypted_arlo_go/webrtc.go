package scrypted_arlo_go

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	"github.com/pion/webrtc/v3"
)

var UDP_PACKET_SIZE = 1600

// type aliases for gopy to detect these structs
type WebRTCConfiguration = webrtc.Configuration
type WebRTCICEServer = webrtc.ICEServer
type WebRTCSessionDescription = webrtc.SessionDescription
type WebRTCICECandidateInit = webrtc.ICECandidateInit
type WebRTCICECandidate = webrtc.ICECandidate

func NewWebRTCSDPType(sdpType string) webrtc.SDPType {
	return webrtc.NewSDPType(sdpType)
}

func NewWebRTCICEServer(urls []string, username string, credential string) WebRTCICEServer {
	return WebRTCICEServer{
		URLs:       urls,
		Username:   username,
		Credential: credential,
	}
}

func WebRTCSessionDescriptionSDP(s WebRTCSessionDescription) string {
	return s.SDP
}

const (
	// copy these values for gopy to detect these variables
	WebRTCMimeTypeOpus = webrtc.MimeTypeOpus
	WebRTCMimeTypePCMA = webrtc.MimeTypePCMA
	WebRTCMimeTypePCMU = webrtc.MimeTypePCMU
	WebRTCMimeTypeH264 = webrtc.MimeTypeH264
)

type WebRTCManager struct {
	// name to use in logging messages
	name string

	pc *webrtc.PeerConnection

	// for receiving audio RTP packets
	audioRTP net.Conn

	// used to signal completion of ice gathering
	// cache results in iceCandidates
	iceCompleteSentinel <-chan struct{}
	iceCandidates       []WebRTCICECandidate
}

func NewWebRTCManager(name string, cfg WebRTCConfiguration, apiSettings ...func(*webrtc.API)) (*WebRTCManager, error) {
	mgr := WebRTCManager{
		name: name,
	}
	var err error
	if len(apiSettings) == 0 {
		mgr.pc, err = webrtc.NewPeerConnection(cfg)
	} else {
		api := webrtc.NewAPI(apiSettings...)
		mgr.pc, err = api.NewPeerConnection(cfg)
	}
	if err != nil {
		return nil, err
	}
	mgr.iceCompleteSentinel = webrtc.GatheringCompletePromise(mgr.pc)
	mgr.pc.OnICECandidate(func(c *WebRTCICECandidate) {
		if c != nil {
			mgr.iceCandidates = append(mgr.iceCandidates, *c)
		}
	})
	mgr.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		mgr.Println("OnConnectionStateChange %s", s.String())
		if s == webrtc.PeerConnectionStateDisconnected {
			mgr.Close()
		}
	})
	mgr.pc.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		mgr.Println("OnICEConnectionStateChange %s", is.String())
	})
	return &mgr, nil
}

func (mgr *WebRTCManager) Printf(msg string, args ...any) {
	fmt.Printf(fmt.Sprintf("[%s] ", mgr.name)+msg, args...)
}

func (mgr *WebRTCManager) Println(msg string, args ...any) {
	mgr.Printf(msg+"\n", args...)
}

func (mgr *WebRTCManager) initializeRTPListener(kind, codecMimeType string) (conn net.Conn, port int, err error) {
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

	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: codecMimeType}, kind, "pion-"+kind)

	rtpSender, err := mgr.pc.AddTrack(track)
	if err != nil {
		return conn, 0, err
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		rtcpBuf := make([]byte, 1500)
		for {
			if _, _, rtcpErr := rtpSender.Read(rtcpBuf); rtcpErr != nil {
				return
			}
		}
	}()

	go func() {
		// wait for ice to complete gathering
		<-mgr.iceCompleteSentinel

		inboundRTPPacket := make([]byte, UDP_PACKET_SIZE)
		for {
			n, _, err := conn.(*net.UDPConn).ReadFrom(inboundRTPPacket)
			if err != nil {
				mgr.Println("Error during %s track read: %s", kind, err)
				return
			}

			if _, err = track.Write(inboundRTPPacket[:n]); err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					// peerConnection has been closed.
					return
				}

				mgr.Println("Error writing to %s track: %s", kind, err)
				return
			}
		}
	}()

	port, err = strconv.Atoi(strings.Split(conn.LocalAddr().String(), ":")[1])
	if err != nil {
		return conn, 0, err
	}

	mgr.Println("Created %s RTP listener at udp://127.0.0.1:%d", kind, port)
	return conn, port, nil
}

func (mgr *WebRTCManager) InitializeAudioRTPListener(codecMimeType string) (port int, err error) {
	conn, port, err := mgr.initializeRTPListener("audio", codecMimeType)
	if err != nil {
		return 0, err
	}
	mgr.audioRTP = conn
	return port, err
}

func (mgr *WebRTCManager) CreateOffer() (WebRTCSessionDescription, error) {
	return mgr.pc.CreateOffer(nil)
}

func (mgr *WebRTCManager) CreateAnswer() (WebRTCSessionDescription, error) {
	return mgr.pc.CreateAnswer(nil)
}

func (mgr *WebRTCManager) SetLocalDescription(desc WebRTCSessionDescription) error {
	return mgr.pc.SetLocalDescription(desc)
}

func (mgr *WebRTCManager) SetRemoteDescription(desc WebRTCSessionDescription) error {
	return mgr.pc.SetRemoteDescription(desc)
}

func (mgr *WebRTCManager) AddICECandidate(c WebRTCICECandidateInit) error {
	return mgr.pc.AddICECandidate(c)
}

func (mgr *WebRTCManager) WaitAndGetICECandidates() []WebRTCICECandidate {
	mgr.Println("Waiting for ICE candidate gathering to finish")
	<-mgr.iceCompleteSentinel
	mgr.Println("ICE candidate gathering complete")
	return mgr.iceCandidates
}

func (mgr *WebRTCManager) Close() {
	if mgr.audioRTP != nil {
		mgr.audioRTP.Close()
		mgr.audioRTP = nil
	}
	if mgr.pc != nil {
		mgr.pc.Close()
		mgr.pc = nil
	}
}
