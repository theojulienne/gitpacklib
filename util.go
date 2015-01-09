package gitpacklib

import (
	"fmt"
	"io/ioutil"

	"golang.org/x/crypto/ssh"
)

func ParseKeysFromFile(conf *ssh.ServerConfig, filename string) error {
	pemBytes, err := ioutil.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("Failed to read private key file: %s", err.Error())
	}

	err = ParseKeysFromBytes(conf, pemBytes)
	if err != nil {
		return fmt.Errorf("Failed to parse and load private key: %s", err.Error())
	}

	return nil
}

func ParseKeysFromBytes(conf *ssh.ServerConfig, pemBytes []byte) error {
	signer, err := ssh.ParsePrivateKey(pemBytes)
	if err != nil {
		return fmt.Errorf("Could not parse private key: %s", err.Error())
	}

	conf.AddHostKey(signer)

	return nil
}
