package scrypted_arlo_go

import (
	"crypto/md5"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jart/gosip/sdp"
	"github.com/jart/gosip/sip"
	"github.com/jart/gosip/util"
	"github.com/pion/webrtc/v3"
	"golang.org/x/exp/slices"
	"golang.org/x/net/websocket"
)

// https://stackoverflow.com/a/22892986
func randStringImpl(n int, characters []rune) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = characters[rand.Intn(len(characters))]
	}
	return string(b)
}

func randString(n int) string {
	return randStringImpl(n, []rune("abcdefghijklmnopqrstuvwxyz0123456789"))
}

func randDigits(n int) string {
	return randStringImpl(n, []rune("0123456789"))
}

func genBranch() string {
	return "z9hG4bK" + randDigits(7)
}

func isValidCandidate(candidate string) bool {
	return !strings.Contains(candidate, ":") && !strings.Contains(candidate, ".local")
}

type Duration = time.Duration

type SIPInfo struct {
	// arlo device id to call
	DeviceID string

	// sip call information
	CallerURI string
	CalleeURI string
	Password  string
	UserAgent string

	// parsed versions of caller and callee
	from *sip.URI
	to   *sip.URI

	// websocket information
	WebsocketURI     string
	WebsocketHeaders HeadersMap
	WebsocketOrigin  string

	// optional SDP from the caller
	// NOTE: if an SDP is provided, it is assumed that the caller
	// will manage the media traffic, and this SIP client is only
	// used to manage signaling
	SDP string
}

type HeadersMap map[string]string

func (h HeadersMap) toHTTPHeaders() http.Header {
	result := http.Header{}
	for k, v := range h {
		result[k] = []string{v}
	}
	return result
}

type AuthHeader struct {
	Mode   string
	Params map[string]string
}

func md5Digest(args ...string) string {
	hash := md5.New()
	io.WriteString(hash, strings.Join(args, ":"))
	return fmt.Sprintf("%x", hash.Sum(nil))
}

// This function assumes Digest with MD5
func (h *AuthHeader) UpdateResponseDigest(method, password string) error {
	if algorithm, ok := h.Params["algorithm"]; !ok || algorithm != "MD5" {
		return fmt.Errorf("cannot compute response digest with algorithm %q", algorithm)
	}
	if qop, ok := h.Params["qop"]; !ok || qop != "auth" {
		return fmt.Errorf("cannot compute response digest with qop %q", qop)
	}

	if _, ok := h.Params["username"]; !ok {
		return fmt.Errorf("no username found in auth header params")
	}
	if _, ok := h.Params["realm"]; !ok {
		return fmt.Errorf("no realm found in auth header params")
	}
	if _, ok := h.Params["uri"]; !ok {
		return fmt.Errorf("no uri found in auth header params")
	}
	if _, ok := h.Params["nonce"]; !ok {
		return fmt.Errorf("no nonce found in auth header params")
	}
	if _, ok := h.Params["cnonce"]; !ok {
		return fmt.Errorf("no cnonce found in auth header params")
	}
	if _, ok := h.Params["nc"]; !ok {
		return fmt.Errorf("no nc found in auth header params")
	}

	ha1 := md5Digest(h.Params["username"], h.Params["realm"], password)
	ha2 := md5Digest(method, h.Params["uri"])
	response := md5Digest(ha1, h.Params["nonce"], h.Params["nc"], h.Params["cnonce"], h.Params["qop"], ha2)
	h.Params["response"] = response

	return nil
}

func (h AuthHeader) String() string {
	params := []string{}
	for k, v := range h.Params {
		if k == "algorithm" || k == "qop" || k == "nc" {
			params = append(params, fmt.Sprintf("%s=%s", k, v))
		} else {
			params = append(params, fmt.Sprintf("%s=\"%s\"", k, v))
		}
	}
	return fmt.Sprintf("%s %s", h.Mode, strings.Join(params, ", "))
}

func ParseAuthHeader(header string) (AuthHeader, error) {
	if !strings.HasPrefix(header, "Digest") {
		return AuthHeader{}, fmt.Errorf("unsupported header mode, expected 'Digest'")
	}

	kvs := strings.Split(header[7:], ",")
	params := map[string]string{}
	for _, kv := range kvs {
		kv = strings.TrimSpace(kv)
		tokens := strings.Split(kv, "=")
		if len(tokens) < 2 {
			return AuthHeader{}, fmt.Errorf("could not parse header param %q", kv)
		}

		k, v := tokens[0], tokens[1]
		if strings.Contains(kv, "=\"") {
			v = v[1 : len(v)-1]
		}

		params[k] = v
	}

	if alg, ok := params["algorithm"]; !ok || alg != "MD5" {
		return AuthHeader{}, fmt.Errorf("unsupported auth digest %q", alg)
	}

	return AuthHeader{
		Mode:   "Digest",
		Params: params,
	}, nil
}

type SIPWebRTCManager struct {
	webrtc  *WebRTCManager
	sipInfo SIPInfo

	wsConn          *websocket.Conn
	tlsKeylogWriter io.WriteCloser

	randHost string
	timeout  Duration

	inviteResp        *sip.Msg
	inviteRespMsgLock *sync.Mutex
}

func NewSIPWebRTCManager(infoLoggerPort, debugLoggerPort int, iceServers []WebRTCICEServer, sipInfo SIPInfo) (*SIPWebRTCManager, error) {
	wm, err := newWebRTCManager(infoLoggerPort, debugLoggerPort, iceServers, "SIPWebRTCManager")
	if err != nil {
		return nil, err
	}

	sm := &SIPWebRTCManager{
		webrtc:            wm,
		sipInfo:           sipInfo,
		inviteRespMsgLock: &sync.Mutex{},
		randHost:          randString(12) + ".invalid",
		timeout:           5 * time.Second,
	}
	sm.sipInfo.from, err = sip.ParseURI([]byte(sm.sipInfo.CallerURI))
	if err != nil {
		return nil, fmt.Errorf("could not parse caller uri: %w", err)
	}
	sm.sipInfo.to, err = sip.ParseURI([]byte(sm.sipInfo.CalleeURI))
	if err != nil {
		return nil, fmt.Errorf("could not parse callee uri: %w", err)
	}

	wm.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateDisconnected {
			sm.Close()
		}
	})

	return sm, nil
}

func (sm *SIPWebRTCManager) Info(msg string, args ...any) {
	if sm.webrtc != nil {
		sm.webrtc.Info(msg, args...)
	}
}

func (sm *SIPWebRTCManager) Debug(msg string, args ...any) {
	if sm.webrtc != nil {
		sm.webrtc.Debug(msg, args...)
	}
}

/*
func (sm *SIPWebRTCManager) DebugDumpKeys(outputDir string) error {
	if !DEBUG {
		return nil
	}

	err := sm.webrtc.DebugDumpKeys(outputDir)
	if err != nil {
		return err
	}

	filePath := path.Join(outputDir, "smTLS.log")
	f, err := os.Create(filePath)
	if err != nil {
		return fmt.Errorf("could not create TLS keylog: %w", err)
	}
	sm.tlsKeylogWriter = f

	return nil
}
*/

func (sm *SIPWebRTCManager) InitializeAudioRTPListener(codecMimeType string) (port int, err error) {
	return sm.webrtc.InitializeAudioRTPListener(codecMimeType)
}

func (sm *SIPWebRTCManager) connectWebsocket() error {
	cfg, err := websocket.NewConfig(sm.sipInfo.WebsocketURI, sm.sipInfo.WebsocketOrigin)
	if err != nil {
		return fmt.Errorf("could not create websocket config: %w", err)
	}
	cfg.Header = sm.sipInfo.WebsocketHeaders.toHTTPHeaders()
	cfg.Protocol = []string{"sip"}

	/*
		if DEBUG && sm.tlsKeylogWriter != nil {
			cfg.TlsConfig = &tls.Config{KeyLogWriter: sm.tlsKeylogWriter}
		}
	*/

	sm.wsConn, err = websocket.DialConfig(cfg)
	if err != nil {
		return fmt.Errorf("could not dial websocket: %w", err)
	}

	return nil
}

func (sm *SIPWebRTCManager) makeLocalSDP() (string, error) {
	offer, err := sm.webrtc.pc.CreateOffer(&webrtc.OfferOptions{OfferAnswerOptions: webrtc.OfferAnswerOptions{VoiceActivityDetection: true}})
	if err != nil {
		return "", fmt.Errorf("could not create offer sdp: %w", err)
	}

	err = sm.webrtc.SetLocalDescription(offer)
	if err != nil {
		return "", fmt.Errorf("could not set local description: %w", err)
	}

	for end := error(nil); end == nil; _, end = sm.webrtc.GetNextICECandidate() {
		continue
	}
	offer = *sm.webrtc.pc.LocalDescription()

	return offer.SDP, nil
}

func (sm *SIPWebRTCManager) makeInvite(localSDP string) *sip.Msg {
	invite := &sip.Msg{
		CallID:     util.GenerateCallID(),
		CSeq:       1,
		Method:     sip.MethodInvite,
		CSeqMethod: sip.MethodInvite,
		Request:    sm.sipInfo.to.Copy(),
		Allow:      "ACK,CANCEL,INVITE,MESSAGE,BYE,OPTIONS,INFO,NOTIFY,REFER",
		XHeader: &sip.XHeader{
			Name:  "X-extension",
			Value: []byte(sm.sipInfo.DeviceID + "; User-Agent: webrtc"),
		},
		Supported: "outbound",
		Via: &sip.Via{
			Host:      sm.randHost,
			Port:      5060,
			Param:     &sip.Param{Name: "branch", Value: genBranch()},
			Transport: "WSS",
		},
		From: &sip.Addr{
			Display: "WebRTC-UDP",
			Uri:     sm.sipInfo.from.Copy(),
			Param:   &sip.Param{Name: "tag", Value: util.GenerateTag()},
		},
		To: &sip.Addr{
			Uri: sm.sipInfo.to.Copy(),
		},
		Contact: &sip.Addr{
			Uri: &sip.URI{
				Scheme: "sip",
				User:   randString(8),
				Host:   sm.randHost + ";transport=ws;ob",
			},
		},
		UserAgent: sm.sipInfo.UserAgent,
		Payload: &sip.MiscPayload{
			T: sdp.ContentType,
			D: []byte(localSDP),
		},
	}

	return invite
}

func (sm *SIPWebRTCManager) verify100Trying(msg *sip.Msg) error {
	if !msg.IsResponse() || msg.Status != sip.StatusTrying {
		return fmt.Errorf("did not receive 100 trying, got %d %s", msg.Status, msg.Phrase)
	}
	return nil
}

func (sm *SIPWebRTCManager) verify200OK(msg *sip.Msg) error {
	if !msg.IsResponse() || msg.Status != sip.StatusOK {
		return fmt.Errorf("did not receive 200 ok, got %d %s", msg.Status, msg.Phrase)
	}
	return nil
}

func (sm *SIPWebRTCManager) verify202Accepted(msg *sip.Msg) error {
	if !msg.IsResponse() || msg.Status != sip.StatusAccepted {
		return fmt.Errorf("did not receive 202 accepted, got %d %s", msg.Status, msg.Phrase)
	}
	return nil
}

func (sm *SIPWebRTCManager) verify407ProxyAuthenticationRequired(msg *sip.Msg) error {
	if !msg.IsResponse() || msg.Status != sip.StatusProxyAuthenticationRequired {
		return fmt.Errorf("did not receive 407 proxy authentication required, got %d %s", msg.Status, msg.Phrase)
	}
	return nil
}

func (sm *SIPWebRTCManager) makeAck(msg *sip.Msg) *sip.Msg {
	via := msg.Via.Copy()
	via.Param = &sip.Param{Name: "branch", Value: genBranch()}
	via.Port = 5060 // hack to stop port from being serialized

	route := msg.RecordRoute.Copy()
	route = route.Reversed()

	from := msg.From.Copy()
	from.Display = "WebRTC-UDP"

	return &sip.Msg{
		CallID:     msg.CallID,
		CSeq:       msg.CSeq,
		Method:     sip.MethodAck,
		CSeqMethod: sip.MethodAck,
		Request:    sm.sipInfo.to.Copy(),
		Route:      route,
		Via:        via,
		From:       from,
		To:         msg.To.Copy(),
		Supported:  "outbound",
		UserAgent:  sm.sipInfo.UserAgent,
	}
}

func (sm *SIPWebRTCManager) makeBye(msg *sip.Msg) *sip.Msg {
	ack := sm.makeAck(msg)
	ack.Method = sip.MethodBye
	ack.CSeqMethod = sip.MethodBye
	ack.CSeq++
	return ack
}

func (sm *SIPWebRTCManager) makeMessage(payload string) *sip.Msg {
	message := &sip.Msg{
		CallID:     util.GenerateCallID(),
		CSeq:       1,
		Method:     sip.MethodMessage,
		CSeqMethod: sip.MethodMessage,
		Request:    sm.sipInfo.to.Copy(),
		Supported:  "outbound",
		Via: &sip.Via{
			Host:      sm.randHost,
			Port:      5060,
			Param:     &sip.Param{Name: "branch", Value: genBranch()},
			Transport: "WSS",
		},
		From: &sip.Addr{
			Uri:   sm.sipInfo.from.Copy(),
			Param: &sip.Param{Name: "tag", Value: util.GenerateTag()},
		},
		To: &sip.Addr{
			Uri: sm.sipInfo.to.Copy(),
		},
		UserAgent: sm.sipInfo.UserAgent,
		Payload: &sip.MiscPayload{
			T: "text/plain",
			D: []byte(payload),
		},
	}
	return message
}

func (sm *SIPWebRTCManager) writeWebsocket(msg *sip.Msg) error {
	msgStr := msg.String()
	msgStr = strings.ReplaceAll(msgStr, "WebRTC-UDP", "\"WebRTC-UDP\"")
	sm.Debug("Sending sip message:\n%s", msgStr)
	sm.wsConn.SetWriteDeadline(time.Now().Add(sm.timeout))
	_, err := sm.wsConn.Write([]byte(msgStr))
	return err
}

func (sm *SIPWebRTCManager) readWebsocket() (*sip.Msg, error) {
	var readBuf = make([]byte, 4096)

	sm.wsConn.SetReadDeadline(time.Now().Add(sm.timeout))
	n, err := sm.wsConn.Read(readBuf)
	if err != nil {
		return nil, fmt.Errorf("could not read websocket: %w", err)
	}

	sm.Debug("Got sip response:\n%s", string(readBuf[0:n]))

	msg, err := sip.ParseMsg(readBuf[0:n])
	if err != nil {
		return nil, fmt.Errorf("could not parse sip message: %w", err)
	}

	if msg.Payload != nil && msg.Payload.ContentType() == sdp.ContentType {
		// gosip's sdp parsing is buggy, so this workaround is to force a parsing
		// that retains the original data
		patched := strings.Replace(string(readBuf[0:n]), fmt.Sprintf("Content-Type: %s", sdp.ContentType), fmt.Sprintf("Content-Type: %s", "application/sdp1"), 1)
		msg, err = sip.ParseMsg([]byte(patched))
		if err != nil {
			return nil, fmt.Errorf("could not parse patched sip message: %w", err)
		}
		msg.Payload.(*sip.MiscPayload).T = sdp.ContentType
	}

	return msg, nil
}

func (sm *SIPWebRTCManager) sendAck(msg *sip.Msg) error {
	return sm.writeWebsocket(sm.makeAck(msg))
}

func (sm *SIPWebRTCManager) Start() (remoteSDP string, err error) {
	if sm.sipInfo.SDP == "" && sm.webrtc.audioRTP == nil {
		return "", fmt.Errorf("audio rtp listener not initialized")
	}

	defer func() {
		if err != nil {
			sm.Close()
		}
	}()

	if err = sm.connectWebsocket(); err != nil {
		return "", fmt.Errorf("could not connect websocket: %w", err)
	}

	var localSDP string = sm.sipInfo.SDP
	if localSDP == "" {
		// need to generate sdp
		localSDP, err = sm.makeLocalSDP()
		if err != nil {
			return "", fmt.Errorf("could not create local sdp: %w", err)
		}

		tokens := strings.Split(localSDP, "\r\n")
		tokens = slices.DeleteFunc[[]string](tokens, func(s string) bool {
			if strings.HasPrefix(s, "a=candidate:") && !isValidCandidate(s) {
				sm.Debug("Filtered out candidate: %s", s)
				return true
			}
			return false
		})
		localSDP = strings.Join(tokens, "\r\n")
	}

	invite := sm.makeInvite(localSDP)
	if err = sm.writeWebsocket(invite); err != nil {
		return "", fmt.Errorf("could not send invite over websocket: %w", err)
	}

	trying, err := sm.readWebsocket()
	if err != nil {
		return "", fmt.Errorf("could not read invite response: %w", err)
	}
	if err = sm.verify100Trying(trying); err != nil {
		return "", fmt.Errorf("could not parse 100 trying: %w", err)
	}

	inviteResponse, err := sm.readWebsocket()
	if err != nil {
		return "", fmt.Errorf("could not read invite response: %w", err)
	}
	if sm.verify407ProxyAuthenticationRequired(inviteResponse) == nil {
		// for 407, we need to respond with an ack then add the auth header to the invite
		if err := sm.sendAck(inviteResponse); err != nil {
			return "", fmt.Errorf("could not send ack: %w", err)
		}

		authHeader, err := ParseAuthHeader(inviteResponse.ProxyAuthenticate)
		if err != nil {
			return "", fmt.Errorf("could not parse Proxy-Authenticate from 407 response: %w", err)
		}

		// this is what it looks like in an arlo web negotiation
		authHeader.Params["username"] = sm.sipInfo.from.User
		authHeader.Params["uri"] = sm.sipInfo.CalleeURI
		authHeader.Params["cnonce"] = randString(12)
		authHeader.Params["nc"] = "00000001"
		authHeader.UpdateResponseDigest(sip.MethodInvite, sm.sipInfo.Password)

		invite.ProxyAuthorization = authHeader.String()
		invite.Via.Param = &sip.Param{Name: "branch", Value: genBranch()}
		invite.CSeq++

		if err = sm.writeWebsocket(invite); err != nil {
			return "", fmt.Errorf("could not send invite over websocket: %w", err)
		}

		trying, err = sm.readWebsocket()
		if err != nil {
			return "", fmt.Errorf("could not read invite response: %w", err)
		}
		if err = sm.verify100Trying(trying); err != nil {
			return "", fmt.Errorf("could not parse 100 trying: %w", err)
		}

		inviteResponse, err = sm.readWebsocket()
		if err != nil {
			return "", fmt.Errorf("could not read invite response: %w", err)
		}
	}
	if err = sm.verify200OK(inviteResponse); err != nil {
		return "", fmt.Errorf("could not parse 200 ok: %w", err)
	}

	sm.inviteRespMsgLock.Lock()
	sm.inviteResp = inviteResponse
	sm.inviteRespMsgLock.Unlock()

	if inviteResponse.Payload.ContentType() != sdp.ContentType {
		return "", fmt.Errorf("unexpected invite response content type %q", inviteResponse.Payload.ContentType())
	}

	remoteSDP = string(inviteResponse.Payload.Data())

	if !strings.Contains(remoteSDP, "a=mid:") {
		lines := strings.Split(remoteSDP, "\r\n")
		for idx, line := range lines {
			if strings.HasPrefix(line, "m=audio") {
				lines = append(lines[:idx+2], lines[idx+1:]...)
				lines[idx+1] = "a=mid:0"
				break
			}
		}
		for idx, line := range lines {
			if strings.HasPrefix(line, "m=video") {
				lines = append(lines[:idx+2], lines[idx+1:]...)
				lines[idx+1] = "a=mid:1"
				break
			}
		}
		remoteSDP = strings.Join(lines, "\r\n")
	}
	if !strings.Contains(remoteSDP, "a=sendrecv") {
		lines := strings.Split(remoteSDP, "\r\n")
		for idx, line := range lines {
			if strings.HasPrefix(line, "m=audio") {
				lines = append(lines[:idx+2], lines[idx+1:]...)
				lines[idx+1] = "a=sendrecv"
				break
			}
		}
		for idx, line := range lines {
			if strings.HasPrefix(line, "m=video") {
				lines = append(lines[:idx+2], lines[idx+1:]...)
				lines[idx+1] = "a=sendrecv"
				break
			}
		}
		remoteSDP = strings.Join(lines, "\r\n")
	}

	if sm.sipInfo.SDP == "" {
		err = sm.webrtc.SetRemoteDescription(WebRTCSessionDescription{
			Type: webrtc.SDPTypeAnswer,
			SDP:  remoteSDP,
		})
		if err != nil {
			return "", fmt.Errorf("could not set remote description: %w", err)
		}
	}

	if err = sm.sendAck(inviteResponse); err != nil {
		return "", fmt.Errorf("could not send ack: %w", err)
	}

	if sm.sipInfo.SDP == "" {
		startTalk := sm.makeMessage(fmt.Sprintf("deviceId:%s;startTalk", sm.sipInfo.DeviceID))
		if err = sm.writeWebsocket(startTalk); err != nil {
			return "", fmt.Errorf("could not send startTalk over websocket: %w", err)
		}
	}

	keepAlive := sm.makeMessage("keepAlive")
	if err = sm.writeWebsocket(keepAlive); err != nil {
		return "", fmt.Errorf("could not send keepAlive over websocket: %w", err)
	}

	if sm.sipInfo.SDP == "" {
		startTalkResponse, err := sm.readWebsocket()
		if err != nil {
			return "", fmt.Errorf("could not read startTalk response: %w", err)
		}
		if err = sm.verify202Accepted(startTalkResponse); err != nil {
			return "", fmt.Errorf("could not parse 202 accepted: %w", err)
		}
	}

	keepAliveResponse, err := sm.readWebsocket()
	if err != nil {
		return "", fmt.Errorf("could not read keepAlive response: %w", err)
	}
	if err = sm.verify202Accepted(keepAliveResponse); err != nil {
		return "", fmt.Errorf("could not parse 202 accepted: %w", err)
	}

	// keepAlive loop
	go func() {
		for {
			time.Sleep(30 * time.Second)

			keepAlive := sm.makeMessage("keepAlive")
			if err = sm.writeWebsocket(keepAlive); err != nil {
				sm.Info("Could not send keepAlive over websocket: %s", err)
				break
			}

			keepAliveResponse, err := sm.readWebsocket()
			if err != nil {
				sm.Info("Could not read keepAlive response: %s", err)
				break
			} else if err = sm.verify202Accepted(keepAliveResponse); err != nil {
				sm.Info("Could not parse 202 accepted: %s", err)
				break
			}
		}
		sm.Close()
	}()

	if sm.sipInfo.SDP == "" {
		sm.Info("Started SIP push to talk")
		sm.webrtc.PrintTimeSinceCreation()
	}

	return remoteSDP, nil
}

func (sm *SIPWebRTCManager) StartTalk() error {
	startTalk := sm.makeMessage(fmt.Sprintf("deviceId:%s;startTalk", sm.sipInfo.DeviceID))
	if err := sm.writeWebsocket(startTalk); err != nil {
		return fmt.Errorf("could not send startTalk over websocket: %w", err)
	}
	startTalkResponse, err := sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read startTalk response: %w", err)
	}
	if err = sm.verify202Accepted(startTalkResponse); err != nil {
		return fmt.Errorf("could not parse 202 accepted: %w", err)
	}
	return nil
}

func (sm *SIPWebRTCManager) StopTalk() error {
	stopTalk := sm.makeMessage(fmt.Sprintf("deviceId:%s;stopTalk", sm.sipInfo.DeviceID))
	if err := sm.writeWebsocket(stopTalk); err != nil {
		return fmt.Errorf("could not send startTalk over websocket: %w", err)
	}
	stopTalkResponse, err := sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read startTalk response: %w", err)
	}
	if err = sm.verify202Accepted(stopTalkResponse); err != nil {
		return fmt.Errorf("could not parse 202 accepted: %w", err)
	}
	return nil
}

func (sm *SIPWebRTCManager) Close() {
	sm.inviteRespMsgLock.Lock()
	defer sm.inviteRespMsgLock.Unlock()

	if sm.inviteResp != nil {
		bye := sm.makeBye(sm.inviteResp)
		sm.writeWebsocket(bye)
	}

	sm.wsConn.Close()

	if sm.tlsKeylogWriter != nil {
		sm.tlsKeylogWriter.Close()
	}

	if sm.sipInfo.SDP == "" {
		sm.webrtc.Close()
	}
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
