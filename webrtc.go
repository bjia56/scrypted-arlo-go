package scrypted_arlo_go

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"

	webrtc "github.com/pion/webrtc/v3"
)

var UDP_PACKET_SIZE = 1600

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
	WebRTCMimeTypeOpus = webrtc.MimeTypeOpus
	WebRTCMimeTypePCMA = webrtc.MimeTypePCMA
	WebRTCMimeTypePCMU = webrtc.MimeTypePCMU
	WebRTCMimeTypeH264 = webrtc.MimeTypeH264
)

type WebRTCManager struct {
	name                string
	pc                  *webrtc.PeerConnection
	audioRTP            net.Conn
	videoRTP            net.Conn
	iceCompleteSentinel <-chan struct{}
	iceCandidates       []WebRTCICECandidate
}

func NewWebRTCManager(name string, cfg WebRTCConfiguration) (*WebRTCManager, error) {
	mgr := WebRTCManager{
		name: name,
	}
	var err error
	mgr.pc, err = webrtc.NewPeerConnection(cfg)
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
		mgr.Printf("OnConnectionStateChange %s\n", s.String())
	})
	mgr.pc.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		mgr.Printf("OnICEConnectionStateChange %s\n", is.String())
	})
	return &mgr, nil
}

func (mgr *WebRTCManager) Printf(msg string, args ...any) {
	fmt.Printf(fmt.Sprintf("[%s] ", mgr.name)+msg, args...)
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
				mgr.Printf("Error during %s track read: %s\n", kind, err)
				return
			}

			if _, err = track.Write(inboundRTPPacket[:n]); err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					// peerConnection has been closed.
					return
				}

				mgr.Printf("Error writing to %s track: %s\n", kind, err)
				return
			}
		}
	}()

	port, err = strconv.Atoi(strings.Split(conn.LocalAddr().String(), ":")[1])
	if err != nil {
		return conn, 0, err
	}

	mgr.Printf("Created %s RTP listener at udp://127.0.0.1:%d\n", kind, port)
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

func (mgr *WebRTCManager) InitializeVideoRTPListener(codecMimeType string) (port int, err error) {
	conn, port, err := mgr.initializeRTPListener("video", codecMimeType)
	if err != nil {
		return 0, err
	}
	mgr.videoRTP = conn
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
	mgr.Printf("Waiting for ICE candidate gathering to finish\n")
	<-mgr.iceCompleteSentinel
	mgr.Printf("ICE candidate gathering complete\n")
	return mgr.iceCandidates
}

func (mgr *WebRTCManager) ForwardAudioTo(dst *WebRTCManager) {
	outputTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: WebRTCMimeTypeOpus}, "audio", "pion-forwarder")
	if err != nil {
		mgr.Printf("Error creating forwarding output track: %s\n", err)
		return
	}

	rtpSender, err := dst.pc.AddTrack(outputTrack)
	if err != nil {
		mgr.Printf("Error adding output track: %s\n", err)
		return
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

	alreadyForwarded := false
	mgr.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		mgr.Printf("Track has started: %s %s\n", track.Kind().String(), track.Codec().MimeType)
		if track.Kind() != webrtc.RTPCodecTypeAudio {
			return
		}

		if track.Codec().MimeType != WebRTCMimeTypeOpus {
			mgr.Printf("Cannot forward non-opus track, skipping this one")
			return
		}

		if alreadyForwarded {
			mgr.Printf("Already forwaring a track, skipping this one")
			return
		}
		alreadyForwarded = true

		go func() {
			for {
				rtp, _, err := track.ReadRTP()
				if err != nil {
					mgr.Printf("Error reading RTP from track: %s\n", err)
					return
				}

				mgr.Printf("Read RTP packet of payload length %d", len(rtp.Payload))

				if err = outputTrack.WriteRTP(rtp); err != nil {
					mgr.Printf("Error writing RTP to forwarding output track: %s\n", err)
					return
				}
			}
		}()
	})
}

func (mgr *WebRTCManager) Close() {
	if mgr.audioRTP != nil {
		mgr.audioRTP.Close()
		mgr.audioRTP = nil
	}
	if mgr.videoRTP != nil {
		mgr.videoRTP.Close()
		mgr.videoRTP = nil
	}
	if mgr.pc != nil {
		mgr.pc.Close()
		mgr.pc = nil
	}
}
