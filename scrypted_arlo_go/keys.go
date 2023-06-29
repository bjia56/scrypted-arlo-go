package scrypted_arlo_go

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
)

type KeysOutput struct {
	PublicPEM  string
	PrivatePEM string
}

func GenerateRSAKeys(bitsize int) (KeysOutput, error) {
	priv, err := rsa.GenerateKey(rand.Reader, bitsize)
	if err != nil {
		return KeysOutput{}, fmt.Errorf("could not generate key: %w", err)
	}

	err = priv.Validate()
	if err != nil {
		return KeysOutput{}, fmt.Errorf("could not validate key: %w", err)
	}

	privPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: x509.MarshalPKCS1PrivateKey(priv),
		},
	)
	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PUBLIC KEY",
			Bytes: x509.MarshalPKCS1PublicKey(&priv.PublicKey),
		},
	)

	return KeysOutput{
		PublicPEM:  string(pubPEM),
		PrivatePEM: string(privPEM),
	}, nil
}
