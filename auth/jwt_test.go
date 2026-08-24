package auth_test

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"testing"
	"time"

	"github.com/a-h/depot/auth"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/ssh"
)

var keyTypes = []string{"rsa", "ecdsa-p256", "ecdsa-p384", "ecdsa-p521", "ed25519"}

func newPrivateKey(keyType string) (crypto.Signer, error) {
	switch keyType {
	case "rsa":
		return rsa.GenerateKey(rand.Reader, 2048)
	case "ecdsa-p256":
		return ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case "ecdsa-p384":
		return ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	case "ecdsa-p521":
		return ecdsa.GenerateKey(elliptic.P521(), rand.Reader)
	case "ed25519":
		_, key, err := ed25519.GenerateKey(rand.Reader)
		return key, err
	}
	return nil, fmt.Errorf("unknown key type: %s", keyType)
}

func newSigner(keyType string) (ssh.Signer, error) {
	key, err := newPrivateKey(keyType)
	if err != nil {
		return nil, err
	}
	return ssh.NewSignerFromKey(key)
}

// createEd25519Token creates a token with arbitrary claims, for testing tokens that CreateJWT would not issue.
func createEd25519Token(key ed25519.PrivateKey, claims auth.JWTClaims) (string, error) {
	signingString, err := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims).SigningString()
	if err != nil {
		return "", err
	}
	return signingString + "." + base64.RawURLEncoding.EncodeToString(ed25519.Sign(key, []byte(signingString))), nil
}

func TestTokensSignedBySSHKeysAreVerified(t *testing.T) {
	for _, keyType := range keyTypes {
		t.Run(keyType+" keys create tokens that verify", func(t *testing.T) {
			signer, err := newSigner(keyType)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}
			config := &auth.AuthConfig{
				Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: signer.PublicKey()}},
			}

			token, err := auth.CreateJWT(signer)
			if err != nil {
				t.Fatalf("failed to create token: %v", err)
			}

			fingerprint, err := auth.VerifyJWT(token, config)
			if err != nil {
				t.Fatalf("failed to verify token: %v", err)
			}
			if expected := ssh.FingerprintSHA256(signer.PublicKey()); fingerprint != expected {
				t.Errorf("expected fingerprint %q, got %q", expected, fingerprint)
			}
		})
	}
}

func TestTokensUseTheAlgorithmThatMatchesTheKey(t *testing.T) {
	tests := []struct {
		keyType   string
		algorithm string
	}{
		{keyType: "rsa", algorithm: "RS256"},
		{keyType: "ecdsa-p256", algorithm: "ES256"},
		{keyType: "ecdsa-p384", algorithm: "ES384"},
		{keyType: "ecdsa-p521", algorithm: "ES512"},
		{keyType: "ed25519", algorithm: "EdDSA"},
	}
	for _, test := range tests {
		t.Run(test.keyType+" keys are signed with "+test.algorithm, func(t *testing.T) {
			signer, err := newSigner(test.keyType)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}

			token, err := auth.CreateJWT(signer)
			if err != nil {
				t.Fatalf("failed to create token: %v", err)
			}

			parsed, _, err := jwt.NewParser().ParseUnverified(token, &auth.JWTClaims{})
			if err != nil {
				t.Fatalf("failed to parse token: %v", err)
			}
			if parsed.Method.Alg() != test.algorithm {
				t.Errorf("expected algorithm %q, got %q", test.algorithm, parsed.Method.Alg())
			}
		})
	}
}

func TestTokensSignedByUnauthorizedKeysAreRejected(t *testing.T) {
	for _, keyType := range keyTypes {
		t.Run(keyType+" keys that are not in the auth file are rejected", func(t *testing.T) {
			authorized, err := newSigner(keyType)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}
			unauthorized, err := newSigner(keyType)
			if err != nil {
				t.Fatalf("failed to create signer: %v", err)
			}
			config := &auth.AuthConfig{
				Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: authorized.PublicKey()}},
			}

			token, err := auth.CreateJWT(unauthorized)
			if err != nil {
				t.Fatalf("failed to create token: %v", err)
			}

			if _, err = auth.VerifyJWT(token, config); err == nil {
				t.Fatal("expected verification to fail, but it succeeded")
			}
		})
	}
}

func TestTokensThatClaimAnotherKeysFingerprintAreRejected(t *testing.T) {
	_, attackerKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	attacker, err := ssh.NewSignerFromKey(attackerKey)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}
	victim, err := newSigner("ed25519")
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}

	config := &auth.AuthConfig{
		Keys: []auth.AuthorizedKey{
			{Permission: auth.PermissionReadWrite, PublicKey: victim.PublicKey()},
			{Permission: auth.PermissionRead, PublicKey: attacker.PublicKey()},
		},
	}

	token, err := createEd25519Token(attackerKey, auth.JWTClaims{
		KeyFingerprint: ssh.FingerprintSHA256(victim.PublicKey()),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	if _, err = auth.VerifyJWT(token, config); err == nil {
		t.Fatal("expected verification to fail, but it succeeded")
	}
}

func TestExpiredTokensAreRejected(t *testing.T) {
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}

	config := &auth.AuthConfig{
		Keys: []auth.AuthorizedKey{{Permission: auth.PermissionReadWrite, PublicKey: signer.PublicKey()}},
	}

	token, err := createEd25519Token(key, auth.JWTClaims{
		KeyFingerprint: ssh.FingerprintSHA256(signer.PublicKey()),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now().Add(-48 * time.Hour)),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(-24 * time.Hour)),
		},
	})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	if _, err = auth.VerifyJWT(token, config); err == nil {
		t.Fatal("expected verification to fail, but it succeeded")
	}
}

func TestSupportedKeyTypesAreReported(t *testing.T) {
	for _, keyType := range keyTypes {
		signer, err := newSigner(keyType)
		if err != nil {
			t.Fatalf("failed to create signer: %v", err)
		}
		if !auth.IsSupportedKeyType(signer.PublicKey()) {
			t.Errorf("expected key type %s to be supported", keyType)
		}
	}

	signer, err := newSigner("ed25519")
	if err != nil {
		t.Fatalf("failed to create signer: %v", err)
	}
	// FIDO2 and certificate keys produce signatures that JWT cannot verify.
	certificate := &ssh.Certificate{Key: signer.PublicKey(), CertType: ssh.UserCert}
	if auth.IsSupportedKeyType(certificate) {
		t.Errorf("expected key type %q to be unsupported", certificate.Type())
	}
}
