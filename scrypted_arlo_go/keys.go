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

	privKeyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return KeysOutput{}, fmt.Errorf("could not marshal key: %w", err)
	}
	pubKeyBytes, err := x509.MarshalPKIXPublicKey(priv.Public())
	if err != nil {
		return KeysOutput{}, fmt.Errorf("could not marshal key: %w", err)
	}

	privPEM := pem.EncodeToMemory(
		&pem.Block{
			Type:  "PRIVATE KEY",
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
