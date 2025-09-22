package sri

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"fmt"
	"hash"
	"strings"
)

type Algorithm string

const (
	MD5    Algorithm = "md5"
	SHA1   Algorithm = "sha1"
	SHA256 Algorithm = "sha256"
	SHA512 Algorithm = "sha512"
)

type SRI struct {
	Algorithm Algorithm
	Hasher    hash.Hash
}

func (sri *SRI) Write(p []byte) (n int, err error) {
	return sri.Hasher.Write(p)
}

func (sri *SRI) Sum(b []byte) []byte {
	return sri.Hasher.Sum(b)
}

func (sri *SRI) Reset() {
	sri.Hasher.Reset()
}

func (sri *SRI) String() string {
	return fmt.Sprintf("%s-%s", sri.Algorithm, base64.StdEncoding.EncodeToString(sri.Hasher.Sum(nil)))
}

func Parse(s string) (sri *SRI, err error) {
	// Example: sha256-<hash>.
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid SRI format")
	}
	hasher, err := newHasher(Algorithm(parts[0]))
	if err != nil {
		return nil, err
	}
	sri = &SRI{
		Algorithm: Algorithm(parts[0]),
		Hasher:    hasher,
	}
	return sri, nil
}

func New(algorithm Algorithm) (sri SRI, err error) {
	hasher, err := newHasher(algorithm)
	if err != nil {
		return SRI{}, err
	}
	return SRI{Algorithm: algorithm, Hasher: hasher}, nil
}

func newHasher(algorithm Algorithm) (hasher hash.Hash, err error) {
	switch algorithm {
	case MD5:
		return md5.New(), nil
	case SHA1:
		return sha1.New(), nil
	case SHA256:
		return sha256.New(), nil
	case SHA512:
		return sha512.New(), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm: %s", algorithm)
	}
}
