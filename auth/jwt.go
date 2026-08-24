package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/ssh"
)

// JWTClaims represents the claims in our JWT tokens.
type JWTClaims struct {
	KeyFingerprint string `json:"key_fingerprint"`
	jwt.RegisteredClaims
}

// CreateJWT creates a JWT token signed by an SSH key.
func CreateJWT(signer ssh.Signer) (string, error) {
	publicKey := signer.PublicKey()

	method, algorithm, err := signingMethod(publicKey)
	if err != nil {
		return "", err
	}

	claims := JWTClaims{
		KeyFingerprint: ssh.FingerprintSHA256(publicKey),
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}

	signingString, err := jwt.NewWithClaims(method, claims).SigningString()
	if err != nil {
		return "", fmt.Errorf("failed to get signing string: %w", err)
	}

	// SSH signers hash the message themselves, so the signing string is passed unhashed.
	signature, err := signSSH(signer, algorithm, []byte(signingString))
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}
	if signature.Format != algorithm {
		return "", fmt.Errorf("expected signature format %q, got %q", algorithm, signature.Format)
	}

	encodedSignature, err := jwtSignature(publicKey, signature)
	if err != nil {
		return "", err
	}

	return strings.Join([]string{signingString, base64.RawURLEncoding.EncodeToString(encodedSignature)}, "."), nil
}

// signingMethod returns the JWT signing method and the SSH signature algorithm to use for a key.
func signingMethod(publicKey ssh.PublicKey) (method jwt.SigningMethod, algorithm string, err error) {
	switch publicKey.Type() {
	case ssh.KeyAlgoRSA:
		// SSH defaults RSA keys to SHA-1, which JWT does not support.
		return jwt.SigningMethodRS256, ssh.KeyAlgoRSASHA256, nil
	case ssh.KeyAlgoECDSA256:
		return jwt.SigningMethodES256, ssh.KeyAlgoECDSA256, nil
	case ssh.KeyAlgoECDSA384:
		return jwt.SigningMethodES384, ssh.KeyAlgoECDSA384, nil
	case ssh.KeyAlgoECDSA521:
		return jwt.SigningMethodES512, ssh.KeyAlgoECDSA521, nil
	case ssh.KeyAlgoED25519:
		return jwt.SigningMethodEdDSA, ssh.KeyAlgoED25519, nil
	}
	return nil, "", fmt.Errorf("unsupported SSH key type: %s", publicKey.Type())
}

// signSSH signs data with the given SSH signature algorithm.
func signSSH(signer ssh.Signer, algorithm string, data []byte) (*ssh.Signature, error) {
	if algorithm == signer.PublicKey().Type() {
		return signer.Sign(rand.Reader, data)
	}
	algorithmSigner, ok := signer.(ssh.AlgorithmSigner)
	if !ok {
		return nil, fmt.Errorf("signer does not support the %s algorithm", algorithm)
	}
	return algorithmSigner.SignWithAlgorithm(rand.Reader, data, algorithm)
}

// jwtSignature converts an SSH signature blob into the encoding expected by JWT.
func jwtSignature(publicKey ssh.PublicKey, signature *ssh.Signature) ([]byte, error) {
	switch publicKey.Type() {
	case ssh.KeyAlgoRSA, ssh.KeyAlgoED25519:
		return signature.Blob, nil
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		// SSH stores ECDSA signatures as two mpints, JWT as fixed-width R and S.
		var parsed struct{ R, S *big.Int }
		if err := ssh.Unmarshal(signature.Blob, &parsed); err != nil {
			return nil, fmt.Errorf("failed to parse ECDSA signature: %w", err)
		}
		size, err := ecdsaComponentSize(publicKey.Type())
		if err != nil {
			return nil, err
		}
		if parsed.R.BitLen() > size*8 || parsed.S.BitLen() > size*8 {
			return nil, fmt.Errorf("ECDSA signature components are too large for key type %s", publicKey.Type())
		}
		out := make([]byte, size*2)
		parsed.R.FillBytes(out[:size])
		parsed.S.FillBytes(out[size:])
		return out, nil
	}
	return nil, fmt.Errorf("unsupported SSH key type: %s", publicKey.Type())
}

// ecdsaComponentSize returns the byte length of each half of an ECDSA signature.
func ecdsaComponentSize(keyType string) (int, error) {
	switch keyType {
	case ssh.KeyAlgoECDSA256:
		return 32, nil
	case ssh.KeyAlgoECDSA384:
		return 48, nil
	case ssh.KeyAlgoECDSA521:
		return 66, nil
	}
	return 0, fmt.Errorf("unsupported SSH key type: %s", keyType)
}

// VerifyJWT verifies a JWT token and returns the key fingerprint if valid.
func VerifyJWT(tokenString string, authConfig *AuthConfig) (string, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (any, error) {
		switch token.Method.(type) {
		case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA, *jwt.SigningMethodEd25519:
			// These are acceptable.
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		claims, ok := token.Claims.(*JWTClaims)
		if !ok {
			return nil, fmt.Errorf("invalid claims type")
		}

		for _, authKey := range authConfig.Keys {
			if ssh.FingerprintSHA256(authKey.PublicKey) != claims.KeyFingerprint {
				continue
			}
			cryptoKey, err := extractCryptoPublicKey(authKey.PublicKey)
			if err != nil {
				return nil, fmt.Errorf("failed to extract crypto key: %w", err)
			}
			return cryptoKey, nil
		}

		return nil, fmt.Errorf("key not found in authorized keys")
	})
	if err != nil {
		return "", fmt.Errorf("failed to verify JWT: %w", err)
	}
	if !token.Valid {
		return "", fmt.Errorf("token is not valid")
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok {
		return "", fmt.Errorf("invalid claims type")
	}

	return claims.KeyFingerprint, nil
}

// extractCryptoPublicKey extracts a crypto.PublicKey from an SSH public key.
func extractCryptoPublicKey(sshKey ssh.PublicKey) (crypto.PublicKey, error) {
	key, ok := sshKey.(ssh.CryptoPublicKey)
	if !ok {
		return nil, fmt.Errorf("SSH key does not implement CryptoPublicKey")
	}
	cryptoKey := key.CryptoPublicKey()

	switch sshKey.Type() {
	case ssh.KeyAlgoRSA:
		rsaKey, ok := cryptoKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("failed to cast to RSA public key")
		}
		return rsaKey, nil
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		ecdsaKey, ok := cryptoKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("failed to cast to ECDSA public key")
		}
		return ecdsaKey, nil
	case ssh.KeyAlgoED25519:
		ed25519Key, ok := cryptoKey.(ed25519.PublicKey)
		if !ok {
			return nil, fmt.Errorf("failed to cast to ed25519 public key")
		}
		return ed25519Key, nil
	}
	return nil, fmt.Errorf("unsupported SSH key type: %s", sshKey.Type())
}

// IsSupportedKeyType reports whether an SSH key can be used to create JWT tokens.
func IsSupportedKeyType(publicKey ssh.PublicKey) bool {
	_, _, err := signingMethod(publicKey)
	return err == nil
}
