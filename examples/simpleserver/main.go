package main

import (
	"log"

	"github.com/theojulienne/gitpacklib"
	"golang.org/x/crypto/ssh"
)

type DummyClientHandler struct {
}

func (h *DummyClientHandler) NewClient() gitpacklib.Client {
	return &DummyClient{}
}

type DummyClient struct {
	key ssh.PublicKey
}

func (h *DummyClient) AuthenticatePublicKey(conn ssh.ConnMetadata, key ssh.PublicKey) (bool, error) {
	log.Println("Authenticating with public key:", key, conn)
	return true, nil
}

func (h *DummyClient) PublicKeyChosen(key ssh.PublicKey) {
	log.Println("Authenticated with key:", key)
	h.key = key
}

func (c *DummyClient) GetRepositoryBackingStore(repoPath string) (gitpacklib.BackingStore, error) {
	log.Println("Authenticating client against repo and providing backing store:", c, repoPath)
	store, err := gitpacklib.NewFileBackingStore("_gitdata")
	return store, err
}

func main() {
	var clientHandler = DummyClientHandler{}
	var config = &gitpacklib.ServerConfig{
		SSHPort: 2222,
		SSHConfig: ssh.ServerConfig{
			NoClientAuth: false,
		},
		ClientHandler: &clientHandler,
	}

	gitpacklib.ParseKeysFromFile(&config.SSHConfig, "server.key")

	err := gitpacklib.RunServer(config)
	if err != nil {
		log.Fatalln(err)
	}
}
