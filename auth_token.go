package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

const (
	AuthTokenTTL   = 24 * time.Hour
	AuthNonceBytes = 16
	AuthTokenParts = 3
)

func MintAuthToken() (string, error) {
	nonce := make([]byte, AuthNonceBytes)

	_, err := rand.Read(nonce)
	if err != nil {
		return "", err
	}

	expiry := time.Now().Add(AuthTokenTTL).Unix()
	nonceHex := hex.EncodeToString(nonce)
	expStr := strconv.FormatInt(expiry, 10)

	payload := nonceHex + "." + expStr
	mac := AuthHMAC(payload)

	return payload + "." + hex.EncodeToString(mac), nil
}

func VerifyAuthToken(token string) bool {
	parts := strings.Split(token, ".")
	if len(parts) != AuthTokenParts {
		return false
	}

	nonceHex := parts[0]
	expStr := parts[1]
	macHex := parts[2]

	if len(nonceHex) != AuthNonceBytes*2 {
		return false
	}

	_, err := hex.DecodeString(nonceHex)
	if err != nil {
		return false
	}

	mac, err := hex.DecodeString(macHex)
	if err != nil || len(mac) != sha256.Size {
		return false
	}

	exp, err := strconv.ParseInt(expStr, 10, 64)
	if err != nil || exp <= 0 {
		return false
	}

	if time.Now().Unix() > exp {
		return false
	}

	payload := nonceHex + "." + expStr
	expected := AuthHMAC(payload)

	return hmac.Equal(mac, expected)
}

func AuthHMAC(payload string) []byte {
	hm := hmac.New(sha256.New, []byte(config.Tokens.Auth))

	hm.Write([]byte(payload))

	return hm.Sum(nil)
}

func CreateConfigAuthToken(bytes int) (string, error) {
	token := make([]byte, bytes)

	_, err := rand.Read(token)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(token), nil
}
