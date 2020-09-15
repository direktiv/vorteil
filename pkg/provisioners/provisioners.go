package provisioners

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"io"

	"github.com/sisatech/goapi/pkg/file"
	"github.com/vorteil/vorteil/pkg/vdisk"
)

const (
	MapKey = "type"
)

type Provisioner interface {
	Type() string
	DiskFormat() vdisk.Format
	Initialize(data []byte) error
	Provision(args *ProvisionArgs) error
	ProvisionVolume(args *ProvisionVolumeArgs) error
	Marshal() ([]byte, error)
}

type ProvisionArgs struct {
	Name string
	// Logger // TODO
	Description     string
	Force           bool
	Context         context.Context
	Image           file.File
	ReadyWhenUsable bool
}

type ProvisionVolumeArgs struct {
	ProvisionArgs
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
