package scrypted_arlo_go

import (
	"fmt"
	"math/rand"
	"net/http"
	"net/url"
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
}

func ToHTTPHeaders(headers map[string]string) http.Header {
	result := http.Header{}
	for k, v := range headers {
		result[k] = []string{v}
	}
	return result
}

type SIPWebRTCManager struct {
	*WebRTCManager
	sipInfo SIPInfo

	wsConn   *websocket.Conn
	randHost string

	inviteSIPMsg     *sip.Msg
	inviteSIPMsgLock *sync.Mutex
}

func NewSIPWebRTCManager(name string, cfg WebRTCConfiguration, sipInfo SIPInfo) (*SIPWebRTCManager, error) {
	webrtc, err := NewWebRTCManager(name, cfg)
	if err != nil {
		return nil, err
	}
	if sipInfo.TimeoutSeconds <= 0 {
		sipInfo.TimeoutSeconds = 5
	}
	sm := &SIPWebRTCManager{
		WebRTCManager:    webrtc,
		sipInfo:          sipInfo,
		inviteSIPMsgLock: &sync.Mutex{},
		randHost:         RandString(12) + ".invalid",
	}
	return sm, nil
}

func (sm *SIPWebRTCManager) connectWebsocket() error {
	wsLocation, err := url.Parse(sm.sipInfo.WebsocketURI)
	if err != nil {
		return fmt.Errorf("could not parse websocket uri: %w", err)
	}

	sm.wsConn, err = websocket.DialConfig(&websocket.Config{
		Location: wsLocation,
		Header:   sm.sipInfo.WebsocketHeaders,
		Protocol: []string{"sip"},
	})
	if err != nil {
		return fmt.Errorf("could not dial websocket: %w", err)
	}

	return nil
}

func (sm *SIPWebRTCManager) makeLocalSDP() (string, error) {
	offer, err := sm.CreateOffer()
	if err != nil {
		return "", fmt.Errorf("could not create offer sdp: %w", err)
	}

	err = sm.SetLocalDescription(offer)
	if err != nil {
		return "", fmt.Errorf("could not set local description: %w", err)
	}

	sm.WaitAndGetICECandidates()
	offer = *sm.pc.LocalDescription()

	return offer.SDP, nil
}

func (sm *SIPWebRTCManager) makeInvite(sdp string) (*sip.Msg, error) {
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

	sm.inviteSIPMsgLock.Lock()
	defer sm.inviteSIPMsgLock.Unlock()
	sm.inviteSIPMsg = invite

	return invite, nil
}

func (sm *SIPWebRTCManager) verify100Trying(msg *sip.Msg) error {
	if !msg.IsResponse() || msg.Status != sip.StatusTrying || msg.Phrase != "Trying" {
		return fmt.Errorf("did not receive 100 trying, got %d %s", msg.Status, msg.Phrase)
	}
	return nil
}

func (sm *SIPWebRTCManager) verify200OK(msg *sip.Msg) error {
	if !msg.IsResponse() || msg.Status != sip.StatusOK || msg.Phrase != "OK" {
		return fmt.Errorf("did not receive 200 ok, got %d %s", msg.Status, msg.Phrase)
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
	sm.wsConn.SetWriteDeadline(time.Now().Add(sm.sipInfo.TimeoutSeconds * time.Second))
	_, err := sm.wsConn.Write([]byte(msg.String()))
	return err
}

func (sm *SIPWebRTCManager) readWebsocket() (*sip.Msg, error) {
	var readBuf = make([]byte, 2048)

	sm.wsConn.SetReadDeadline(time.Now().Add(sm.sipInfo.TimeoutSeconds * time.Second))
	n, err := sm.wsConn.Read(readBuf)
	if err != nil {
		return nil, fmt.Errorf("could not read websocket: %w", err)
	}

	sm.Println("Got sip response:\n", string(readBuf[0:n]))

	msg, err := sip.ParseMsg(readBuf[0:n])
	if err != nil {
		return nil, fmt.Errorf("could not parse sip message: %w", err)
	}

	return msg, nil
}

func (sm *SIPWebRTCManager) Start() error {
	if sm.audioRTP == nil {
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
	sm.Println("Arlo offer sdp:\n%s", sdp)

	invite, err := sm.makeInvite(sdp)
	if err != nil {
		return fmt.Errorf("could not create invite: %w", err)
	}
	inviteStr := invite.String()
	sm.Println("Arlo sip invite:\n%s", inviteStr)

	sm.writeWebsocket(invite)
	if err != nil {
		return fmt.Errorf("could not send invite over websocket: %w", err)
	}

	msg, err := sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read invite response: %w", err)
	}
	if err = sm.verify100Trying(msg); err != nil {
		return fmt.Errorf("could not parse 100 trying: %w", err)
	}

	msg, err = sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read invite response: %w", err)
	}
	if err = sm.verify200OK(msg); err != nil {
		return fmt.Errorf("could not parse 200 ok: %w", err)
	}

	err = sm.SetRemoteDescription(WebRTCSessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  string(msg.Payload.Data()),
	})
	if err != nil {
		return fmt.Errorf("could not set remote description: %w", err)
	}

	startTalk := sm.makeMessage(fmt.Sprintf("deviceId:%s;startTalk", sm.sipInfo.DeviceID))
	err = sm.writeWebsocket(startTalk)
	if err != nil {
		return fmt.Errorf("could not send startTalk over websocket: %w", err)
	}

	keepAlive := sm.makeMessage("keepAlive")
	err = sm.writeWebsocket(keepAlive)
	if err != nil {
		return fmt.Errorf("could not send keepAlive over websocket: %w", err)
	}

	msg, err = sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read startTalk response: %w", err)
	}
	if err = sm.verify200OK(msg); err != nil {
		return fmt.Errorf("could not parse 200 ok: %w", err)
	}

	msg, err = sm.readWebsocket()
	if err != nil {
		return fmt.Errorf("could not read keepAlive response: %w", err)
	}
	if err = sm.verify200OK(msg); err != nil {
		return fmt.Errorf("could not parse 200 ok: %w", err)
	}

	sm.Println("Started SIP push to talk")

	return nil
}

func (sm *SIPWebRTCManager) Close() {
	sm.WebRTCManager.Close()
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
