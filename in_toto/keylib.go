package in_toto

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/asn1"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"golang.org/x/crypto/ed25519"
	"io/ioutil"
	"os"
	"strings"
)

const (
	rsaKeyType     string = "rsa"
	ecdsaKeyType   string = "ecdsa"
	ed25519KeyType string = "ed25519"
)

// ErrFailedPEMParsing gets returned when PKCS1, PKCS8 or PKIX key parsing fails
var ErrFailedPEMParsing = errors.New("failed parsing the PEM block: unsupported PEM type")

// ErrNoPEMBlock gets triggered when there is no PEM block in the provided file
var ErrNoPEMBlock = errors.New("failed to decode the data as PEM block (are you sure this is a pem file?)")

// ErrUnsupportedKeyType is returned when we are dealing with a key type different to ed25519 or RSA
var ErrUnsupportedKeyType = errors.New("unsupported key type")

// ErrInvalidSignature is returned when the signature is invalid
var ErrInvalidSignature = errors.New("invalid signature")

// ErrInvalidKeyType is returned when the keytype is invalid for the given key
var ErrInvalidKeyType = errors.New("invalid key type for this key")

/*
GenerateKeyId creates a partial key map and generates the key ID
based on the created partial key map via the SHA256 method.
The resulting keyID will be directly saved in the corresponding key object.
On success GenerateKeyId will return nil, in case of errors while encoding
there will be an error.
*/
func (k *Key) GenerateKeyId() error {
	// Create partial key map used to create the keyid
	// Unfortunately, we can't use the Key object because this also carries
	// yet unwanted fields, such as KeyId and KeyVal.Private and therefore
	// produces a different hash. We generate the keyId exactly as we do in
	// the securesystemslib  to keep interoperability between other in-toto
	// implementations.
	var keyToBeHashed = map[string]interface{}{
		"keytype":               k.KeyType,
		"scheme":                k.Scheme,
		"keyid_hash_algorithms": k.KeyIdHashAlgorithms,
		"keyval": map[string]string{
			"public": k.KeyVal.Public,
		},
	}
	keyCanonical, err := EncodeCanonical(keyToBeHashed)
	if err != nil {
		return err
	}
	// calculate sha256 and return string representation of keyId
	keyHashed := sha256.Sum256(keyCanonical)
	k.KeyId = fmt.Sprintf("%x", keyHashed)
	err = validateKey(*k)
	if err != nil {
		return err
	}
	return nil
}

/*
GeneratePublicPemBlock creates a "PUBLIC KEY" PEM block from public key byte data.
If successful it returns PEM block as []byte slice. This function should always
succeed, if pubKeyBytes is empty the PEM block will have an empty byte block.
Therefore only header and footer will exist.
*/
func GeneratePublicPemBlock(pubKeyBytes []byte) []byte {
	// construct PEM block
	publicKeyPemBlock := &pem.Block{
		Type:    "PUBLIC KEY",
		Headers: nil,
		Bytes:   pubKeyBytes,
	}
	return pem.EncodeToMemory(publicKeyPemBlock)
}

/*
SetKeyComponents sets all components in our key object.
Furthermore it makes sure to remove any trailing and leading whitespaces or newlines.
We treat key types differently for interoperability reasons to the in-toto python
implementation and the securesystemslib.
*/
func (k *Key) SetKeyComponents(pubKeyBytes []byte, privateKeyBytes []byte, keyType string, scheme string, keyIdHashAlgorithms []string) error {
	// assume we have a privateKey if the key size is bigger than 0
	switch keyType {
	case rsaKeyType, ecdsaKeyType:
		if len(privateKeyBytes) > 0 {
			k.KeyVal = KeyVal{
				Private: strings.TrimSpace(string(privateKeyBytes)),
				Public:  strings.TrimSpace(string(GeneratePublicPemBlock(pubKeyBytes))),
			}
		} else {
			k.KeyVal = KeyVal{
				Public: strings.TrimSpace(string(pubKeyBytes)),
			}
		}
	case ed25519KeyType:
		if len(privateKeyBytes) > 0 {
			k.KeyVal = KeyVal{
				Private: strings.TrimSpace(hex.EncodeToString(privateKeyBytes)),
				Public:  strings.TrimSpace(hex.EncodeToString(pubKeyBytes)),
			}
		} else {
			k.KeyVal = KeyVal{
				Public: strings.TrimSpace(hex.EncodeToString(pubKeyBytes)),
			}
		}
	default:
		return fmt.Errorf("%w: %s", ErrUnsupportedKeyType, keyType)
	}
	k.KeyType = keyType
	k.Scheme = scheme
	k.KeyIdHashAlgorithms = keyIdHashAlgorithms
	if err := k.GenerateKeyId(); err != nil {
		return err
	}
	return nil
}

/*
ParseKey tries to parse a PEM []byte slice. Using the following standards
in the given order:

	* PKCS8
	* PKCS1
	* PKIX

On success it returns the parsed key and nil.
On failure it returns nil and the error ErrFailedPEMParsing
*/
func ParseKey(data []byte) (interface{}, error) {
	key, err := x509.ParsePKCS8PrivateKey(data)
	if err == nil {
		return key, nil
	}
	key, err = x509.ParsePKCS1PrivateKey(data)
	if err == nil {
		return key, nil
	}
	key, err = x509.ParsePKIXPublicKey(data)
	if err == nil {
		return key, nil
	}
	return nil, ErrFailedPEMParsing
}

/*
LoadKey loads the key file at specified file path into the key object.
It automatically derives the PEM type and the key type.
Right now the following PEM types are supported:

	* PKCS1 for private keys
	* PKCS8	for private keys
	* PKIX for public keys

The following key types are supported:

	* ed25519
	* RSA
	* ecdsa

On success it will return nil. The following errors can happen:

	* path not found or not readable
	* no PEM block in the loaded file
	* no valid PKCS8/PKCS1 private key or PKIX public key
	* errors while marshalling
	* unsupported key types
*/
func (k *Key) LoadKey(path string, scheme string, keyIdHashAlgorithms []string) error {
	pemFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := pemFile.Close(); closeErr != nil {
			err = closeErr
		}
	}()
	// Read key bytes
	pemBytes, err := ioutil.ReadAll(pemFile)
	if err != nil {
		return err
	}

	// pem.Decode returns the parsed pem block and a rest.
	// The rest is everything, that could not be parsed as PEM block.
	// Therefore we can drop this via using the blank identifier "_"
	data, _ := pem.Decode(pemBytes)
	if data == nil {
		return ErrNoPEMBlock
	}

	// Try to load private key, if this fails try to load
	// key as public key
	key, err := ParseKey(data.Bytes)
	if err != nil {
		return err
	}

	// Use type switch to identify the key format
	switch key.(type) {
	case *rsa.PublicKey:
		if err := k.SetKeyComponents(pemBytes, []byte{}, rsaKeyType, scheme, keyIdHashAlgorithms); err != nil {
			return err
		}
	case *rsa.PrivateKey:
		// Note: RSA Public Keys will get stored as X.509 SubjectPublicKeyInfo (RFC5280)
		// This behavior is consistent to the securesystemslib
		pubKeyBytes, err := x509.MarshalPKIXPublicKey(key.(*rsa.PrivateKey).Public())
		if err != nil {
			return err
		}
		if err := k.SetKeyComponents(pubKeyBytes, pemBytes, rsaKeyType, scheme, keyIdHashAlgorithms); err != nil {
			return err
		}
	case ed25519.PublicKey:
		if err := k.SetKeyComponents(key.(ed25519.PublicKey), []byte{}, ed25519KeyType, scheme, keyIdHashAlgorithms); err != nil {
			return err
		}
	case ed25519.PrivateKey:
		pubKeyBytes := key.(ed25519.PrivateKey).Public()
		if err := k.SetKeyComponents(pubKeyBytes.(ed25519.PublicKey), key.(ed25519.PrivateKey), ed25519KeyType, scheme, keyIdHashAlgorithms); err != nil {
			return err
		}
	case *ecdsa.PrivateKey:
		pubKeyBytes, err := x509.MarshalPKIXPublicKey(key.(*ecdsa.PrivateKey).Public())
		if err != nil {
			return err
		}
		if err := k.SetKeyComponents(pubKeyBytes, pemBytes, ecdsaKeyType, scheme, keyIdHashAlgorithms); err != nil {
			return err
		}
	case *ecdsa.PublicKey:
		if err := k.SetKeyComponents(pemBytes, []byte{}, ecdsaKeyType, scheme, keyIdHashAlgorithms); err != nil {
			return err
		}
	default:
		return fmt.Errorf("%w: %T", ErrUnsupportedKeyType, key)
	}
	return nil
}

/*
GenerateSignature will automatically detect the key type and sign the signable data
with the provided key. If everything goes right GenerateSignature will return
a for the key valid signature and err=nil. If something goes wrong it will
return a not initialized signature and an error. Possible errors are:

	* ErrNoPEMBlock
	* ErrUnsupportedKeyType

Currently supported is only one scheme per key.
*/
func GenerateSignature(signable []byte, key Key) (Signature, error) {
	var signature Signature
	var signatureBuffer []byte
	// The following switch block is needed for keeping interoperability
	// with the securesystemslib and the python implementation
	// in which we are storing RSA keys in PEM format, but ed25519 keys hex encoded.
	switch key.KeyType {
	case rsaKeyType, ecdsaKeyType:
		// pem.Decode returns the parsed pem block and a rest.
		// The rest is everything, that could not be parsed as PEM block.
		// Therefore we can drop this via using the blank identifier "_"
		data, _ := pem.Decode([]byte(key.KeyVal.Private))
		if data == nil {
			return signature, ErrNoPEMBlock
		}
		parsedKey, err := ParseKey(data.Bytes)
		if err != nil {
			return signature, err
		}
		hashed := sha256.Sum256(signable)
		switch parsedKey.(type) {
		case *rsa.PrivateKey:
			if key.KeyType != "rsa" {
				return signature, ErrInvalidKeyType
			}
			// We use rand.Reader as secure random source for rsa.SignPSS()
			signatureBuffer, err = rsa.SignPSS(rand.Reader, parsedKey.(*rsa.PrivateKey), crypto.SHA256, hashed[:],
				&rsa.PSSOptions{SaltLength: sha256.Size, Hash: crypto.SHA256})
			if err != nil {
				return signature, err
			}
		case *ecdsa.PrivateKey:
			if key.KeyType != ecdsaKeyType {
				return signature, ErrInvalidKeyType
			}
			// ecdsa.Sign returns a signature that consists of two components called: r and s
			// We assume here, that r and s are of the same size nLen and that
			// the signature is 2*nLen. Furthermore we must note  that hashes get truncated
			// if they are too long for the curve. We use SHA256 for hashing, thus we should be
			// ok with using the FIPS186-3 curves P256, P384 and P521.
			r, s, err := ecdsa.Sign(rand.Reader, parsedKey.(*ecdsa.PrivateKey), hashed[:])
			if err != nil {
				return signature, nil
			}
			// Generate the ecdsa signature on the same way, as we do in the securesystemslib
			// We are marshalling the ecdsaSignature struct as ASN.1 INTEGER SEQUENCES
			// into an ASN.1 Object.
			signatureBuffer, err = asn1.Marshal(EcdsaSignature{
				R: r,
				S: s,
			})
		default:
			return signature, fmt.Errorf("%w: %T", ErrUnsupportedKeyType, parsedKey)
		}
	case ed25519KeyType:
		privateHex, err := hex.DecodeString(key.KeyVal.Private)
		if err != nil {
			return signature, err
		}
		// Note: We can directly use the key for signing and do not
		// need to use ed25519.NewKeyFromSeed().
		signatureBuffer = ed25519.Sign(privateHex, signable)
	default:
		return signature, fmt.Errorf("%w: %s", ErrUnsupportedKeyType, key.KeyType)
	}
	signature.Sig = hex.EncodeToString(signatureBuffer)
	signature.KeyId = key.KeyId
	return signature, nil
}

/*
VerifySignature will verify unverified byte data via a passed key and signature.
Supported key types are:

	* RSA
	* ED25519
	* ECDSA

When encountering an RSA key, VerifySignature will decode the PEM block in the key
and will call rsa.VerifyPSS() for verifying the RSA signature.
When encountering an ed25519 key, VerifySignature will decode the hex string encoded
public key and will use ed25519.Verify() for verifying the ed25519 signature.
When the given key is an ecdsa key, VerifySignature will unmarshall the ASN1 object
and will use the retrieved ecdsa components 'r' and 's' for verifying the signature.
On success it will return nil. In case of an unsupported key type or any other error
it will return an error.
*/
func VerifySignature(key Key, sig Signature, unverified []byte) error {
	switch key.KeyType {
	case rsaKeyType, ecdsaKeyType:
		// pem.Decode returns the parsed pem block and a rest.
		// The rest is everything, that could not be parsed as PEM block.
		// Therefore we can drop this via using the blank identifier "_"
		data, _ := pem.Decode([]byte(key.KeyVal.Public))
		if data == nil {
			return ErrNoPEMBlock
		}
		parsedKey, err := ParseKey(data.Bytes)
		if err != nil {
			return err
		}
		hashed := sha256.Sum256(unverified)
		sigBytes, _ := hex.DecodeString(sig.Sig)
		switch parsedKey.(type) {
		case *rsa.PublicKey:
			err = rsa.VerifyPSS(parsedKey.(*rsa.PublicKey), crypto.SHA256, hashed[:], sigBytes, &rsa.PSSOptions{SaltLength: sha256.Size, Hash: crypto.SHA256})
			if err != nil {
				return fmt.Errorf("%w: %s", ErrInvalidSignature, err)
			}
		case *ecdsa.PublicKey:
			var ecdsaSignature EcdsaSignature
			// Unmarshal the ASN.1 DER marshalled ecdsa signature to
			// ecdsaSignature. asn1.Unmarshal returns the rest and an error
			// we can skip the rest here..
			_, err := asn1.Unmarshal(sigBytes, &ecdsaSignature)
			if err != nil {
				return err
			}
			// This may fail if a bigger hashing algorithm than SHA256 has been used for generating the signature
			if err := ecdsa.Verify(parsedKey.(*ecdsa.PublicKey), hashed[:], ecdsaSignature.R, ecdsaSignature.S); err == false {
				return ErrInvalidSignature
			}
		default:
			return fmt.Errorf("%w: Key has type %T", ErrInvalidSignature, parsedKey)
		}
	case ed25519KeyType:
		pubHex, err := hex.DecodeString(key.KeyVal.Public)
		if err != nil {
			return err
		}
		sigHex, err := hex.DecodeString(sig.Sig)
		if err != nil {
			return err
		}
		if ok := ed25519.Verify(pubHex, unverified, sigHex); !ok {
			return fmt.Errorf("%w: ed25519", ErrInvalidSignature)
		}
	default:
		return fmt.Errorf("%w: Key has type %s", ErrInvalidSignature, key.KeyType)
	}
	return nil
}
