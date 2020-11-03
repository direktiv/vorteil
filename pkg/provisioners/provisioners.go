package provisioners

/**
 * SPDX-License-Identifier: Apache-2.0
 * Copyright 2020 vorteil.io Pty Ltd
 */

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"

	"github.com/vorteil/vorteil/pkg/vcfg"
	"github.com/vorteil/vorteil/pkg/vdisk"
	"github.com/vorteil/vorteil/pkg/vio"
)

const (
	MapKey = "type"
)

// Provisioner ...
type Provisioner interface {
	Type() string
	DiskFormat() vdisk.Format
	SizeAlign() vcfg.Bytes
	Provision(args *ProvisionArgs) error
	Marshal() ([]byte, error)
}

// ProvisionArgs ...
type ProvisionArgs struct {
	Name            string
	Description     string
	Force           bool
	ReadyWhenUsable bool
	Context         context.Context
	Image           vio.File
}

type InvalidProvisionerError struct {
	Err error
}

func (e *InvalidProvisionerError) Error() string {
	return fmt.Sprintf("provisioner is invalid: %v", e.Err)
}

func createHash(key string) string {
	hasher := md5.New()
	hasher.Write([]byte(key))
	return hex.EncodeToString(hasher.Sum(nil))
}

func Encrypt(data []byte, passphrase string) []byte {
	block, _ := aes.NewCipher([]byte(createHash(passphrase)))
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic(err.Error())
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		panic(err.Error())
	}
	ciphertext := gcm.Seal(nonce, nonce, data, nil)
	return ciphertext
}

func Decrypt(data []byte, passphrase string) ([]byte, error) {
	key := []byte(createHash(passphrase))
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, err
	}
	return plaintext, nil
}

// ProvisionerType : Return Provisioner type as a string
func ProvisionerType(data []byte) (string, error) {
	m := make(map[string]interface{})
	err := json.Unmarshal(data, &m)
	if err != nil {
		return "", err
	}

	ptype, ok := m[MapKey]
	if !ok {
		return "", fmt.Errorf("malformed provisioner file: mapKey not found")
	}

	return fmt.Sprintf("%v", ptype), nil
}
