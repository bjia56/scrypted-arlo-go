module scrypted_arlo_go

go 1.19

require (
	github.com/jart/gosip v0.0.0-20220818224804-29801cedf805
	github.com/pion/webrtc/v3 v3.3.5
)

require (
	github.com/beatgammit/rtsp v0.0.0-20150328165920-b2346852d8f4
	github.com/tmaxmax/go-sse v0.8.0
	golang.org/x/exp v0.0.0-20240325151524-a685a6edb6d8
)

require github.com/wlynxg/anet v0.0.3 // indirect

replace github.com/jart/gosip v0.0.0-20220818224804-29801cedf805 => github.com/bjia56/gosip v0.0.0-20230624042356-af04e85539a6

replace github.com/beatgammit/rtsp v0.0.0-20150328165920-b2346852d8f4 => github.com/bjia56/rtsp v0.0.0-20231211164110-f608a589d75b

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/google/uuid v1.6.0
	github.com/pion/datachannel v1.5.8 // indirect
	github.com/pion/dtls/v2 v2.2.12 // indirect
	github.com/pion/ice/v2 v2.3.36 // indirect
	github.com/pion/interceptor v0.1.38
	github.com/pion/logging v0.2.3
	github.com/pion/mdns v0.0.12 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.15 // indirect
	github.com/pion/rtp v1.8.15
	github.com/pion/sctp v1.8.19 // indirect
	github.com/pion/sdp/v3 v3.0.9 // indirect
	github.com/pion/srtp/v2 v2.0.20 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/transport/v2 v2.2.10 // indirect
	github.com/pion/turn/v2 v2.1.6 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.10.0 // indirect
	golang.org/x/crypto v0.24.0 // indirect
	golang.org/x/net v0.26.0
	golang.org/x/sys v0.21.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
