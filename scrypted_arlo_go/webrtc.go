package scrypted_arlo_go

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/davecgh/go-spew/spew"
	"github.com/pion/interceptor"
	"github.com/pion/logging"
	"github.com/pion/rtp"
	"github.com/pion/webrtc/v3"
	"golang.org/x/exp/slices"
)

func init() {
	spew.Config.DisableMethods = true
}

var DEBUG = !slices.Contains([]string{"", "0"}, os.Getenv("SCRYPTED_ARLO_GO_DEBUG"))

var UDP_PACKET_SIZE = 1200

// type aliases for gopy to detect these structs
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
	pc     *webrtc.PeerConnection
	logger *TCPLogger

	// for receiving audio RTP packets
	audioRTP net.Conn

	// used to signal completion of ice gathering
	// cache results in iceCandidates
	iceCompleteSentinel <-chan struct{}
	iceCandidates       []WebRTCICECandidate
}

func NewWebRTCManager(loggerPort int, iceServers []WebRTCICEServer) (*WebRTCManager, error) {
	return newWebRTCManager(loggerPort, iceServers, "WebRTCManager")
}

func newWebRTCManager(loggerPort int, iceServers []WebRTCICEServer, name string) (*WebRTCManager, error) {
	logger, err := NewTCPLogger(loggerPort, name)
	if err != nil {
		return nil, err
	}

	mgr := WebRTCManager{
		logger: logger,
	}
	mgr.Println("Library version %s built at %s", version, parsedBuildTime.String())

	certificates := []webrtc.Certificate{}

	/*
		if DEBUG {
			// generate RSA key so we can use it with wireshark
			priv, err := rsa.GenerateKey(rand.Reader, 4096)
			if err != nil {
				return nil, fmt.Errorf("could not generate RSA key: %w", err)
			}
			certificate, err := webrtc.GenerateCertificate(priv)
			if err != nil {
				return nil, fmt.Errorf("could not generate DTLS certificate: %w", err)
			}
			certificates = append(certificates, *certificate)
		}
	*/

	m := &webrtc.MediaEngine{}
	if err := m.RegisterDefaultCodecs(); err != nil {
		return nil, err
	}

	i := &interceptor.Registry{}
	if err := webrtc.RegisterDefaultInterceptors(m, i); err != nil {
		return nil, err
	}

	webrtcLogger := logging.NewDefaultLoggerFactory()
	webrtcLogger.Writer = logger
	s := webrtc.SettingEngine{
		LoggerFactory: webrtcLogger,
	}

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m), webrtc.WithInterceptorRegistry(i), webrtc.WithSettingEngine(s))
	mgr.pc, err = api.NewPeerConnection(webrtc.Configuration{
		ICEServers:           iceServers,
		ICETransportPolicy:   webrtc.ICETransportPolicyAll,
		BundlePolicy:         webrtc.BundlePolicyBalanced,
		RTCPMuxPolicy:        webrtc.RTCPMuxPolicyRequire,
		ICECandidatePoolSize: 0,
		Certificates:         certificates,
	})
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
	mgr.pc.OnTrack(func(tr *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		mgr.Println("Remote sent us a track we will ignore: %s", tr.Codec().MimeType)
		// we don't expect any useful audio to come back over the channel,
		// so just read it and move on
		go func() {
			for {
				if _, _, rtcpErr := tr.ReadRTP(); rtcpErr != nil {
					return
				}
			}
		}()
	})

	return &mgr, nil
}

func (mgr *WebRTCManager) Printf(msg string, args ...any) {
	if mgr.logger != nil {
		mgr.logger.Send(fmt.Sprintf(msg, args...))
	}
}

func (mgr *WebRTCManager) Println(msg string, args ...any) {
	mgr.Printf(msg+"\n", args...)
}

/*
func (mgr *WebRTCManager) DebugDumpKeys(outputDir string) error {
	if !DEBUG {
		return nil
	}

	stat, err := os.Stat(outputDir)
	if errors.Is(err, os.ErrNotExist) {
		err := os.MkdirAll(outputDir, 0700)
		if err != nil {
			return fmt.Errorf("cannot create output directory: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("cannot stat directory: %w", err)
	} else if !stat.IsDir() {
		return fmt.Errorf("%s is not a directory", outputDir)
	}

	for i, c := range mgr.pc.GetConfiguration().Certificates {
		pem, err := c.PEM()
		if err != nil {
			return fmt.Errorf("could not export PEM from certificate: %w", err)
		}
		filePath := path.Join(outputDir, fmt.Sprintf("%d.pem", i))
		f, err := os.Create(filePath)
		if err != nil {
			return fmt.Errorf("could not create output file %s: %w", filePath, err)
		}
		f.WriteString(pem)
		f.Close()
	}

	mgr.Println("Certificate(s) and key(s) written to output directory")
	return nil
}
*/

func (mgr *WebRTCManager) initializeRTPListener(kind, codecMimeType string) (conn net.Conn, port int, err error) {
	if mgr.pc == nil {
		return nil, 0, fmt.Errorf("peer connection closed")
	}

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

	track, err := webrtc.NewTrackLocalStaticRTP(webrtc.RTPCodecCapability{MimeType: codecMimeType}, randString(15), randString(15))
	if err != nil {
		return conn, 0, err
	}

	rtpSender, err := mgr.pc.AddTrack(track)
	if err != nil {
		return conn, 0, err
	}

	// Read incoming RTCP packets
	// Before these packets are returned they are processed by interceptors. For things
	// like NACK this needs to be called.
	go func() {
		for {
			pkt, _, err := rtpSender.ReadRTCP()
			if err != nil {
				return
			}
			if DEBUG {
				mgr.Println(spew.Sdump(pkt))
			}
		}
	}()

	go func() {
		// wait for ice to complete gathering
		<-mgr.iceCompleteSentinel

		mark := true

		inboundRTPPacket := make([]byte, UDP_PACKET_SIZE)
		for {
			n, _, err := conn.(*net.UDPConn).ReadFrom(inboundRTPPacket)
			if err != nil {
				if !errors.Is(err, net.ErrClosed) {
					mgr.Println("Error during %s track read: %s", kind, err)
				}
				return
			}

			var pkt rtp.Packet
			if err = pkt.Unmarshal(inboundRTPPacket[:n]); err != nil {
				mgr.Println("Error unmarshaling RTP packet: %s", err)
				continue
			}

			// packets we receive from ffmpeg all have the marker set, which seems to
			// confuse arlo's backend. therefore, we only set the first packet's marker
			pkt.Marker = mark
			if mark {
				mark = false
			}

			if err = track.WriteRTP(&pkt); err != nil {
				if !errors.Is(err, io.ErrClosedPipe) {
					mgr.Println("Error writing to %s track: %s", kind, err)
				}
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
	if mgr.pc == nil {
		return webrtc.SessionDescription{}, fmt.Errorf("peer connection closed")
	}
	return mgr.pc.CreateOffer(nil)
}

func (mgr *WebRTCManager) CreateAnswer() (WebRTCSessionDescription, error) {
	if mgr.pc == nil {
		return webrtc.SessionDescription{}, fmt.Errorf("peer connection closed")
	}
	return mgr.pc.CreateAnswer(nil)
}

func (mgr *WebRTCManager) SetLocalDescription(desc WebRTCSessionDescription) error {
	if mgr.pc == nil {
		return fmt.Errorf("peer connection closed")
	}
	return mgr.pc.SetLocalDescription(desc)
}

func (mgr *WebRTCManager) SetRemoteDescription(desc WebRTCSessionDescription) error {
	if mgr.pc == nil {
		return fmt.Errorf("peer connection closed")
	}
	return mgr.pc.SetRemoteDescription(desc)
}

func (mgr *WebRTCManager) AddICECandidate(c WebRTCICECandidateInit) error {
	if mgr.pc == nil {
		return fmt.Errorf("peer connection closed")
	}
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
	if mgr.logger != nil {
		mgr.logger.Close()
		mgr.logger = nil
	}
}
