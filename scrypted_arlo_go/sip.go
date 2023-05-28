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

	"github.com/jart/gosip/sip"
	"github.com/jart/gosip/util"
	"github.com/pion/webrtc/v3"
	"golang.org/x/net/websocket"
)

var characters = []rune("abcdefghijklmnopqrstuvwxyz0123456789")

// https://stackoverflow.com/a/22892986
func RandString(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = characters[rand.Intn(len(characters))]
	}
	return string(b)
}

type Duration = time.Duration

type SIPInfo struct {
	// arlo device id to call
	DeviceID string

	// sip call information
	CallerURI      string
	CalleeURI      string
	Password       string
	UserAgent      string
	TimeoutSeconds Duration

	// parsed versions of caller and callee
	from *sip.URI
	to   *sip.URI

	// websocket information
	WebsocketURI     string
	WebsocketHeaders http.Header
	WebsocketOrigin  string
}

func ToHTTPHeaders(headers map[string]string) http.Header {
	result := http.Header{}
	for k, v := range headers {
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

	wsConn   *websocket.Conn
	randHost string

	inviteSIPMsg     *sip.Msg
	inviteSIPMsgLock *sync.Mutex
}

func NewSIPWebRTCManager(name string, iceServers []WebRTCICEServer, sipInfo SIPInfo) (*SIPWebRTCManager, error) {
	webrtcSettings := webrtc.SettingEngine{}
	webrtcSettings.SetICETimeouts(5*time.Second, 25*time.Second, 0)

	wm, err := NewWebRTCManager(name, webrtc.Configuration{ICEServers: iceServers}, webrtc.WithSettingEngine(webrtcSettings))
	if err != nil {
		return nil, err
	}

	if sipInfo.TimeoutSeconds <= 0 {
		sipInfo.TimeoutSeconds = 5
	}
	sm := &SIPWebRTCManager{
		webrtc:           wm,
		sipInfo:          sipInfo,
		inviteSIPMsgLock: &sync.Mutex{},
		randHost:         RandString(12) + ".invalid",
	}

	wm.pc.OnConnectionStateChange(func(s webrtc.PeerConnectionState) {
		if s == webrtc.PeerConnectionStateDisconnected {
			sm.Close()
		}
	})

	return sm, nil
}

func (sm *SIPWebRTCManager) Println(msg string, args ...any) {
	sm.webrtc.Println(msg, args...)
}

func (sm *SIPWebRTCManager) InitializeAudioRTPListener(codecMimeType string) (port int, err error) {
	return sm.webrtc.InitializeAudioRTPListener(codecMimeType)
}

func (sm *SIPWebRTCManager) connectWebsocket() error {
	cfg, err := websocket.NewConfig(sm.sipInfo.WebsocketURI, sm.sipInfo.WebsocketOrigin)
	if err != nil {
		return fmt.Errorf("could not create websocket config: %w", err)
	}
	cfg.Header = sm.sipInfo.WebsocketHeaders
	cfg.Protocol = []string{"sip"}

	sm.wsConn, err = websocket.DialConfig(cfg)
	if err != nil {
		return fmt.Errorf("could not dial websocket: %w", err)
	}

	return nil
}

func (sm *SIPWebRTCManager) makeLocalSDP() (string, error) {
	offer, err := sm.webrtc.CreateOffer()
	if err != nil {
		return "", fmt.Errorf("could not create offer sdp: %w", err)
	}

	err = sm.webrtc.SetLocalDescription(offer)
	if err != nil {
		return "", fmt.Errorf("could not set local description: %w", err)
	}

	sm.webrtc.WaitAndGetICECandidates()
	offer = *sm.webrtc.pc.LocalDescription()

	return offer.SDP, nil
}

func (sm *SIPWebRTCManager) makeInvite(sdp string, authNonce string) (*sip.Msg, error) {
	var err error

	sm.sipInfo.from, err = sip.ParseURI([]byte(sm.sipInfo.CallerURI))
	if err != nil {
		return nil, fmt.Errorf("could not parse caller uri: %w", err)
	}
	sm.sipInfo.to, err = sip.ParseURI([]byte(sm.sipInfo.CalleeURI))
	if err != nil {
		return nil, fmt.Errorf("could not parse callee uri: %w", err)
	}

	invite := &sip.Msg{
		CallID:     util.GenerateCallID(),
		CSeq:       util.GenerateCSeq(),
		Method:     sip.MethodInvite,
		CSeqMethod: sip.MethodInvite,
		Request:    sm.sipInfo.to.Copy(),
		Via: &sip.Via{
			Host:      sm.randHost,
			Param:     &sip.Param{Name: "branch", Value: util.GenerateBranch()},
			Transport: "WSS",
		},
		From: &sip.Addr{
			Uri:   sm.sipInfo.from.Copy(),
			Param: &sip.Param{Name: "tag", Value: util.GenerateTag()},
		},
		To: &sip.Addr{
			Uri: sm.sipInfo.to.Copy(),
		},
		Contact: &sip.Addr{
			Uri: &sip.URI{
				Scheme: "sip",
				User:   RandString(8),
				Host:   sm.randHost + ";transport=ws;ob",
			},
		},
		UserAgent: sm.sipInfo.UserAgent,
		Payload: &sip.MiscPayload{
			T: "application/sdp",
			D: []byte(sdp),
		},
	}

	if authNonce != "" {
		invite.ProxyAuthenticate = ""
	}

	sm.inviteSIPMsgLock.Lock()
	defer sm.inviteSIPMsgLock.Unlock()
	sm.inviteSIPMsg = invite

	return invite, nil
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
	return &sip.Msg{
		CallID:     msg.CallID,
		CSeq:       msg.CSeq,
		Method:     sip.MethodAck,
		CSeqMethod: sip.MethodAck,
		Request:    sm.sipInfo.to.Copy(),
		Via:        msg.Via.Copy(),
		From:       msg.From.Copy(),
		To:         msg.To.Copy(),
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
		CSeq:       util.GenerateCSeq(),
		Method:     sip.MethodMessage,
		CSeqMethod: sip.MethodMessage,
		Request:    sm.sipInfo.to.Copy(),
		Via: &sip.Via{
			Host:      sm.randHost,
			Param:     &sip.Param{Name: "branch", Value: util.GenerateBranch()},
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
	if sm.wsConn == nil {
		return fmt.Errorf("websocket closed")
	}
	msgStr := msg.String()
	sm.Println("Sending sip message:\n%s", msgStr)
	sm.wsConn.SetWriteDeadline(time.Now().Add(sm.sipInfo.TimeoutSeconds * time.Second))
	_, err := sm.wsConn.Write([]byte(msgStr))
	return err
}

func (sm *SIPWebRTCManager) readWebsocket() (*sip.Msg, error) {
	if sm.wsConn == nil {
		return nil, fmt.Errorf("websocket closed")
	}

	var readBuf = make([]byte, 4096)

	sm.wsConn.SetReadDeadline(time.Now().Add(sm.sipInfo.TimeoutSeconds * time.Second))
	n, err := sm.wsConn.Read(readBuf)
	if err != nil {
		return nil, fmt.Errorf("could not read websocket: %w", err)
	}

	sm.Println("Got sip response:\n%s", string(readBuf[0:n]))

	msg, err := sip.ParseMsg(readBuf[0:n])
	if err != nil {
		return nil, fmt.Errorf("could not parse sip message: %w", err)
	}

	return msg, nil
}

func (sm *SIPWebRTCManager) sendAck(msg *sip.Msg) error {
	return sm.writeWebsocket(sm.makeAck(msg))
}

func (sm *SIPWebRTCManager) Start() error {
	if sm.webrtc.audioRTP == nil {
		return fmt.Errorf("audio rtp listener not initialized")
	}

	err := sm.connectWebsocket()
	if err != nil {
		return fmt.Errorf("could not connect websocket: %w", err)
	}

	sdp, err := sm.makeLocalSDP()
	if err != nil {
		return fmt.Errorf("could not create local sdp: %w", err)
	}

	invite, err := sm.makeInvite(sdp, "")
	if err != nil {
		return fmt.Errorf("could not create invite: %w", err)
	}

	sm.writeWebsocket(invite)
	if err != nil {
		return fmt.Errorf("could not send invite over websocket: %w", err)
	}

	trying, err := sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read invite response: %w", err)
	}
	if err = sm.verify100Trying(trying); err != nil {
		return fmt.Errorf("could not parse 100 trying: %w", err)
	}

	inviteResponse, err := sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read invite response: %w", err)
	}
	if sm.verify407ProxyAuthenticationRequired(inviteResponse) == nil {
		// for 407, we need to respond with an ack then add the auth header to the invite
		if err := sm.sendAck(inviteResponse); err != nil {
			return fmt.Errorf("could not send ack: %w", err)
		}

		authHeader, err := ParseAuthHeader(inviteResponse.ProxyAuthenticate)
		if err != nil {
			return fmt.Errorf("could not parse Proxy-Authenticate from 407 response: %w", err)
		}

		// this is what it looks like in an arlo web negotiation
		authHeader.Params["username"] = sm.sipInfo.from.User
		authHeader.Params["uri"] = sm.sipInfo.CalleeURI
		authHeader.Params["cnonce"] = RandString(12)
		authHeader.Params["nc"] = "00000001"
		authHeader.UpdateResponseDigest(sip.MethodInvite, sm.sipInfo.Password)

		invite.ProxyAuthorization = authHeader.String()
		invite.CSeq++

		sm.writeWebsocket(invite)
		if err != nil {
			return fmt.Errorf("could not send invite over websocket: %w", err)
		}

		trying, err = sm.readWebsocket()
		if err != nil {
			return fmt.Errorf("could not read invite response: %w", err)
		}
		if err = sm.verify100Trying(trying); err != nil {
			return fmt.Errorf("could not parse 100 trying: %w", err)
		}

		inviteResponse, err = sm.readWebsocket()
		if err != nil {
			return fmt.Errorf("could not read invite response: %w", err)
		}
	}
	if err = sm.verify200OK(inviteResponse); err != nil {
		return fmt.Errorf("could not parse 200 ok: %w", err)
	}

	if inviteResponse.Payload.ContentType() != "application/sdp" {
		return fmt.Errorf("unexpected invite response content type %q", inviteResponse.Payload.ContentType())
	}

	remoteSDP := string(inviteResponse.Payload.Data())
	if !strings.Contains(remoteSDP, "a=mid:") {
		remoteSDP += "a=mid:0\r\n"
	}
	if !strings.Contains(remoteSDP, "a=recvonly") {
		remoteSDP += "a=recvonly\r\n"
	}
	err = sm.webrtc.SetRemoteDescription(WebRTCSessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  remoteSDP,
	})
	if err != nil {
		return fmt.Errorf("could not set remote description: %w", err)
	}

	startTalk := sm.makeMessage(fmt.Sprintf("deviceId:%s;startTalk", sm.sipInfo.DeviceID))
	err = sm.writeWebsocket(startTalk)
	if err != nil {
		return fmt.Errorf("could not send startTalk over websocket: %w", err)
	}

	startTalkResponse, err := sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read startTalk response: %w", err)
	}
	if err = sm.verify202Accepted(startTalkResponse); err != nil {
		return fmt.Errorf("could not parse 202 accepted: %w", err)
	}

	// keepAlive loop
	go func() {
		for {
			keepAlive := sm.makeMessage("keepAlive")
			err = sm.writeWebsocket(keepAlive)
			if err != nil {
				sm.Println("Could not send keepAlive over websocket: %s", err)
				return
			}

			keepAliveResponse, err := sm.readWebsocket()
			if err != nil {
				sm.Println("Could not read keepAlive response: %s", err)
			} else if err = sm.verify200OK(keepAliveResponse); err != nil {
				sm.Println("Could not parse 200 ok: %s", err)
			}

			time.Sleep(30 * time.Second)
		}
	}()

	sm.Println("Started SIP push to talk")

	return nil
}

func (sm *SIPWebRTCManager) Close() {
	sm.webrtc.Close()
	if sm.wsConn != nil {
		sm.inviteSIPMsgLock.Lock()
		defer sm.inviteSIPMsgLock.Unlock()

		if sm.inviteSIPMsg != nil {
			bye := sm.makeBye(sm.inviteSIPMsg)
			sm.writeWebsocket(bye)
		}

		sm.wsConn.Close()
	}
	sm.inviteSIPMsg = nil
	sm.wsConn = nil
}

func init() {
	rand.Seed(time.Now().UnixNano())
}
