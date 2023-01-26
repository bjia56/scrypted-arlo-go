package arlo

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

func SetUDPPacketSize(size int) {
	UDP_PACKET_SIZE = size
}

type WebRTCConfiguration = webrtc.Configuration
type WebRTCICEServer = webrtc.ICEServer
type WebRTCSessionDescription = webrtc.SessionDescription

const (
	WebRTCMimeTypeOpus = webrtc.MimeTypeOpus
	WebRTCMimeTypePCMA = webrtc.MimeTypePCMA
	WebRTCMimeTypePCMU = webrtc.MimeTypePCMU
	WebRTCMimeTypeH264 = webrtc.MimeTypeH264
)

type WebRTCManager struct {
	*webrtc.PeerConnection
	audioRTP            net.Conn
	videoRTP            net.Conn
	iceCompleteSentinel <-chan struct{}
}

func NewWebRTCManager(cfg WebRTCConfiguration) (*WebRTCManager, error) {
	var mgr WebRTCManager
	var err error
	mgr.PeerConnection, err = webrtc.NewPeerConnection(cfg)
	if err != nil {
		return nil, err
	}
	mgr.iceCompleteSentinel = webrtc.GatheringCompletePromise(mgr.PeerConnection)
	return &mgr, nil
}

func (mgr *WebRTCManager) InitializeAudioRTPListener(codecMimeType string) (port int, err error) {
	// cleanup in case of error
	defer func() {
		if err != nil && mgr.audioRTP != nil {
			mgr.audioRTP.Close()
			mgr.audioRTP = nil
		}
	}()

	mgr.audioRTP, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return 0, err
	}

	audioTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: codecMimeType}, "audio", "pion-audio")

	rtpSender, err := mgr.AddTrack(audioTrack)
	if err != nil {
		return 0, err
	}
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
			n, _, err := mgr.audioRTP.(*net.UDPConn).ReadFrom(inboundRTPPacket)
			if err != nil {
				fmt.Printf("error during audioTrack read: %s\n", err)
				return
			}

			if _, err = audioTrack.Write(inboundRTPPacket[:n]); err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					// peerConnection has been closed.
					return
				}

				fmt.Printf("error writing to audioTrack: %s\n", err)
				return
			}
		}
	}()

	port, err = strconv.Atoi(strings.Split(mgr.audioRTP.LocalAddr().String(), ":")[1])
	if err != nil {
		return 0, err
	}
	return port, nil
}

func (mgr *WebRTCManager) InitializeVideoRTPListener(codecMimeType string) (port int, err error) {
	// cleanup in case of error
	defer func() {
		if err != nil && mgr.videoRTP != nil {
			mgr.videoRTP.Close()
			mgr.videoRTP = nil
		}
	}()

	mgr.videoRTP, err = net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 0})
	if err != nil {
		return 0, err
	}

	videoTrack, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: codecMimeType}, "video", "pion-video")

	rtpSender, err := mgr.AddTrack(videoTrack)
	if err != nil {
		return 0, err
	}
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
			n, _, err := mgr.videoRTP.(*net.UDPConn).ReadFrom(inboundRTPPacket)
			if err != nil {
				fmt.Printf("error during videoTrack read: %s\n", err)
				return
			}

			if _, err = videoTrack.Write(inboundRTPPacket[:n]); err != nil {
				if errors.Is(err, io.ErrClosedPipe) {
					// peerConnection has been closed.
					return
				}

				fmt.Printf("error writing to videoTrack: %s\n", err)
				return
			}
		}
	}()

	port, err = strconv.Atoi(strings.Split(mgr.videoRTP.LocalAddr().String(), ":")[1])
	if err != nil {
		return 0, err
	}
	return port, nil
}

func (mgr *WebRTCManager) Close() {
	if mgr.audioRTP != nil {
		mgr.audioRTP.Close()
		mgr.audioRTP = nil
	}
}
