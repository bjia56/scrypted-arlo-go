package scrypted_arlo_go

import (
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

func VerifyCertHostname(cert, hostname string) error {
	p, _ := pem.Decode([]byte(cert))
	if p == nil {
		return fmt.Errorf("could not decode cert PEM")
	}

	c, err := x509.ParseCertificate(p.Bytes)
	if err != nil {
		return fmt.Errorf("could not parse cert: %w", err)
	}

	return c.VerifyHostname(hostname)
}
