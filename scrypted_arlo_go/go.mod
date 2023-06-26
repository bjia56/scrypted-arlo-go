module scrypted_arlo_go

go 1.19

require (
	github.com/jart/gosip v0.0.0-20220818224804-29801cedf805
	github.com/pion/webrtc/v3 v3.2.10
)

replace github.com/pion/ice/v2 v2.3.8 => github.com/bjia56/ice/v2 v2.0.0-20230624041812-5b83f210873b
replace github.com/jart/gosip v0.0.0-20220818224804-29801cedf805 => github.com/bjia56/gosip v0.0.0-20230624042356-af04e85539a6

require (
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/pion/datachannel v1.5.5 // indirect
	github.com/pion/dtls/v2 v2.2.7 // indirect
	github.com/pion/ice/v2 v2.3.8 // indirect
	github.com/pion/interceptor v0.1.17 // indirect
	github.com/pion/logging v0.2.2 // indirect
	github.com/pion/mdns v0.0.7 // indirect
	github.com/pion/randutil v0.1.0 // indirect
	github.com/pion/rtcp v1.2.10 // indirect
	github.com/pion/rtp v1.7.13 // indirect
	github.com/pion/sctp v1.8.7 // indirect
	github.com/pion/sdp/v3 v3.0.6 // indirect
	github.com/pion/srtp/v2 v2.0.15 // indirect
	github.com/pion/stun v0.6.0 // indirect
	github.com/pion/transport/v2 v2.2.1 // indirect
	github.com/pion/turn/v2 v2.1.0 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/stretchr/testify v1.8.4 // indirect
	golang.org/x/crypto v0.10.0 // indirect
	golang.org/x/net v0.11.0
	golang.org/x/sys v0.9.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
