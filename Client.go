package gitpacklib

import (
	"golang.org/x/crypto/ssh"
)

type Client interface {
	AuthenticatePublicKey(conn ssh.ConnMetadata, key ssh.PublicKey) (bool, error)
	PublicKeyChosen(key ssh.PublicKey)
	GetRepositoryBackingStore(repoPath string) (BackingStore, error)
}
