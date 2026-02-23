package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
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

// CreateJWT creates a JWT token signed with a crypto private key.
func CreateJWT(privateKey crypto.Signer, publicKey ssh.PublicKey) (string, error) {
	// Get the SSH fingerprint for the public key.
	fingerprint := ssh.FingerprintSHA256(publicKey)

	// Create claims with 24-hour expiration.
	claims := JWTClaims{
		KeyFingerprint: fingerprint,
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}

	// Determine signing method based on key type.
	var signingMethod jwt.SigningMethod
	switch privateKey.Public().(type) {
	case *rsa.PublicKey:
		signingMethod = jwt.SigningMethodRS256
	case *ecdsa.PublicKey:
		signingMethod = jwt.SigningMethodES256
	default:
		return "", fmt.Errorf("unsupported private key type")
	}

	// Create the token.
	token := jwt.NewWithClaims(signingMethod, claims)

	// Get the signing string (header.payload).
	signingString, err := token.SigningString()
	if err != nil {
		return "", fmt.Errorf("failed to get signing string: %w", err)
	}

	// Hash the signing string with SHA256.
	hash := sha256.Sum256([]byte(signingString))

	// Sign the hash using the crypto.Signer.
	signature, err := privateKey.Sign(nil, hash[:], crypto.SHA256)
	if err != nil {
		return "", fmt.Errorf("failed to sign token: %w", err)
	}

	// Encode the signature as base64url.
	encodedSignature := base64.RawURLEncoding.EncodeToString(signature)

	// Construct the final token: header.payload.signature
	return strings.Join([]string{signingString, encodedSignature}, "."), nil
}

// VerifyJWT verifies a JWT token and returns the key fingerprint if valid.
func VerifyJWT(tokenString string, authConfig *AuthConfig) (string, error) {
	// Parse the token.
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method.
		switch token.Method.(type) {
		case *jwt.SigningMethodRSA, *jwt.SigningMethodECDSA:
			// These are acceptable.
		default:
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}

		// Extract claims to get the key fingerprint.
		claims, ok := token.Claims.(*JWTClaims)
		if !ok {
			return nil, fmt.Errorf("invalid claims type")
		}

		// Find the corresponding public key in our auth config.
		for _, authKey := range authConfig.Keys {
			if ssh.FingerprintSHA256(authKey.PublicKey) == claims.KeyFingerprint {
				// Convert SSH public key to crypto public key for verification.
				cryptoKey, err := extractCryptoPublicKey(authKey.PublicKey)
				if err != nil {
					return nil, fmt.Errorf("failed to extract crypto key: %w", err)
				}
				return cryptoKey, nil
			}
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
	switch sshKey.Type() {
	case ssh.KeyAlgoRSA:
		// Parse the SSH RSA public key to get the crypto/rsa key.
		key, ok := sshKey.(ssh.CryptoPublicKey)
		if !ok {
			return nil, fmt.Errorf("SSH key does not implement CryptoPublicKey")
		}
		cryptoKey := key.CryptoPublicKey()
		rsaKey, ok := cryptoKey.(*rsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("failed to cast to RSA public key")
		}
		return rsaKey, nil
	case ssh.KeyAlgoECDSA256, ssh.KeyAlgoECDSA384, ssh.KeyAlgoECDSA521:
		// Parse the SSH ECDSA public key to get the crypto/ecdsa key.
		key, ok := sshKey.(ssh.CryptoPublicKey)
		if !ok {
			return nil, fmt.Errorf("SSH key does not implement CryptoPublicKey")
		}
		cryptoKey := key.CryptoPublicKey()
		ecdsaKey, ok := cryptoKey.(*ecdsa.PublicKey)
		if !ok {
			return nil, fmt.Errorf("failed to cast to ECDSA public key")
		}
		return ecdsaKey, nil
	default:
		return nil, fmt.Errorf("unsupported SSH key type: %s", sshKey.Type())
	}
}

// ExtractCryptoPublicKey is a public wrapper for extractCryptoPublicKey.
func ExtractCryptoPublicKey(sshKey ssh.PublicKey) (crypto.PublicKey, error) {
	return extractCryptoPublicKey(sshKey)
}
