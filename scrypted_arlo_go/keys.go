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

	privKeyBytes := x509.MarshalPKCS1PrivateKey(priv)
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(priv.Public())
	if err != nil {
		return KeysOutput{}, fmt.Errorf("could not marshal key: %w", err)
	}

	privPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "RSA PRIVATE KEY",
			Bytes: privKeyBytes,
		},
	)
	pubPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PUBLIC KEY",
			Bytes: pubKeyBytes,
		},
	)

	return KeysOutput{
		PublicPEM:  string(pubPEM),
		PrivatePEM: string(privPEM),
	}, nil
}
