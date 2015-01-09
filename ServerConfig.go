package gitpacklib

import (
	"golang.org/x/crypto/ssh"
)

type ServerConfig struct {
	SSHPort   int
	SSHConfig ssh.ServerConfig

	ClientHandler ClientHandler
}
