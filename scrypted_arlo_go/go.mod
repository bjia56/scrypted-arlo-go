module scrypted_arlo_go

go 1.19

require (
	github.com/jart/gosip v0.0.0-20220818224804-29801cedf805
	github.com/pion/webrtc/v3 v3.2.24
)

require (
	github.com/beatgammit/rtsp v0.0.0-20150328165920-b2346852d8f4
	github.com/tmaxmax/go-sse v0.7.0
	golang.org/x/exp v0.0.0-20231110203233-9a3e6036ecaa
)

require github.com/cenkalti/backoff/v4 v4.2.1 // indirect

replace github.com/pion/ice/v2 v2.3.11 => github.com/bjia56/ice/v2 v2.0.0-20230624041812-5b83f210873b

replace github.com/jart/gosip v0.0.0-20220818224804-29801cedf805 => github.com/bjia56/gosip v0.0.0-20230624042356-af04e85539a6

replace github.com/beatgammit/rtsp v0.0.0-20150328165920-b2346852d8f4 => github.com/bjia56/rtsp v0.0.0-20231211164110-f608a589d75b

require (
	github.com/davecgh/go-spew v1.1.1
	github.com/google/uuid v1.5.0
	github.com/pion/datachannel v1.5.5 // indirect
	github.com/pion/dtls/v2 v2.2.7 // indirect
	github.com/pion/ice/v2 v2.3.11 // indirect
	github.com/pion/interceptor v0.1.25
	github.com/pion/logging v0.2.2
	github.com/pion/mdns v0.0.8 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.12 // indirect
	github.com/pion/rtp v1.8.3
	github.com/pion/sctp v1.8.8 // indirect
	github.com/pion/sdp/v3 v3.0.6 // indirect
	github.com/pion/srtp/v2 v2.0.18 // indirect
	github.com/pion/stun v0.6.1 // indirect
	github.com/pion/transport/v2 v2.2.3 // indirect
	github.com/pion/turn/v2 v2.1.3 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.8.4 // indirect
	golang.org/x/crypto v0.18.0 // indirect
	golang.org/x/net v0.20.0
	golang.org/x/sys v0.16.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
