package scrypted_arlo_go

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

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
	pc          *webrtc.PeerConnection
	infoLogger  *TCPLogger
	debugLogger *TCPLogger
	name        string

	// for receiving audio RTP packets
	audioRTP net.Conn

	// used to signal completion of ice gathering
	// cache results in iceCandidates
	iceCompleteSentinel <-chan struct{}
	iceCandidates       chan WebRTCICECandidate

	// for gathering startup metrics
	startTime time.Time
}

func NewWebRTCManager(infoLoggerPort, debugLoggerPort int, iceServers []WebRTCICEServer) (*WebRTCManager, error) {
	return newWebRTCManager(infoLoggerPort, debugLoggerPort, iceServers, "WebRTCManager")
}

func newWebRTCManager(infoLoggerPort, debugLoggerPort int, iceServers []WebRTCICEServer, name string) (*WebRTCManager, error) {
	infoLogger, err := NewTCPLogger(infoLoggerPort, name)
	if err != nil {
		return nil, err
	}
	debugLogger, err := NewTCPLogger(debugLoggerPort, name)
	if err != nil {
		return nil, err
	}

	mgr := WebRTCManager{
		infoLogger:    infoLogger,
		debugLogger:   debugLogger,
		name:          name,
		startTime:     time.Now(),
		iceCandidates: make(chan WebRTCICECandidate),
	}
	mgr.Info("Library version %s built at %s", version, parsedBuildTime.String())

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
	webrtcLogger.Writer = debugLogger
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
			go func(c WebRTCICECandidate) { mgr.iceCandidates <- c }(*c)
		}
	})
	mgr.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		mgr.Debug("OnConnectionStateChange %s", s.String())
		if s == webrtc.PeerConnectionStateDisconnected {
			mgr.Close()
		} else if s == webrtc.PeerConnectionStateConnected {
			mgr.PrintTimeSinceCreation()
		}
	})
	mgr.pc.OnICEConnectionStateChange(func(is webrtc.ICEConnectionState) {
		mgr.Debug("OnICEConnectionStateChange %s", is.String())
		if is == webrtc.ICEConnectionStateConnected {
			mgr.PrintTimeSinceCreation()
		}
	})
	mgr.pc.OnTrack(func(tr *webrtc.TrackRemote, r *webrtc.RTPReceiver) {
		mgr.Debug("Remote sent us a track we will ignore: %s", tr.Codec().MimeType)
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

func (mgr *WebRTCManager) Info(msg string, args ...any) {
	mgr.infoLogger.Send(fmt.Sprintf(msg+"\n", args...))
}

func (mgr *WebRTCManager) Debug(msg string, args ...any) {
	mgr.debugLogger.Send(fmt.Sprintf(msg+"\n", args...))
}

func (mgr *WebRTCManager) PrintTimeSinceCreation() {
	mgr.Debug("Time elapsed since creation of %s: %s", mgr.name, time.Since(mgr.startTime).String())
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
				mgr.Debug(spew.Sdump(pkt))
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
					mgr.Info("Error during %s track read: %s", kind, err)
				}
				return
			}

			var pkt rtp.Packet
			if err = pkt.Unmarshal(inboundRTPPacket[:n]); err != nil {
				mgr.Info("Error unmarshaling RTP packet: %s", err)
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
					mgr.Info("Error writing to %s track: %s", kind, err)
				}
				return
			}
		}
	}()

	port, err = strconv.Atoi(strings.Split(conn.LocalAddr().String(), ":")[1])
	if err != nil {
		return conn, 0, err
	}

	mgr.Info("Created %s RTP listener at udp://127.0.0.1:%d", kind, port)
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

func (mgr *WebRTCManager) WaitForICEComplete() {
	mgr.Info("Waiting for ICE candidate gathering to finish")
	<-mgr.iceCompleteSentinel
	mgr.Info("ICE candidate gathering complete")
}

func (mgr *WebRTCManager) GetNextICECandidate() (WebRTCICECandidate, error) {
	endOfCandidates := fmt.Errorf("no more candidates")

	select {
	case c := <-mgr.iceCandidates:
		return c, nil
	case <-mgr.iceCompleteSentinel:
		return WebRTCICECandidate{}, endOfCandidates
	}
}

func (mgr *WebRTCManager) Close() {
	mgr.audioRTP.Close()
	mgr.pc.Close()
	mgr.PrintTimeSinceCreation()
	mgr.infoLogger.Close()
	mgr.debugLogger.Close()
}
