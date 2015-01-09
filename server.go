package gitpacklib

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"
)

const authenticatedPublicKeyExt = "gitpacklib.publickey"

func RunServer(conf *ServerConfig) error {
	port := 22
	if conf.SSHPort != 0 {
		port = conf.SSHPort
	}

	listener, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return err
	}

	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Println(err)
			continue
		}

		go handleSSHConnection(conn, conf)
	}

	return nil
}

type ClientSession struct {
	conn     net.Conn
	confCopy ssh.ServerConfig

	client Client
	pubKey ssh.PublicKey
}

func handleSSHConnection(conn net.Conn, conf *ServerConfig) {
	session := &ClientSession{}
	session.conn = conn
	session.confCopy = conf.SSHConfig

	session.client = conf.ClientHandler.NewClient()

	defer session.conn.Close()
	session.handle()
}

func (session *ClientSession) handle() {
	// prepare our copy of the config
	if session.confCopy.PublicKeyCallback != nil {
		log.Fatalln("PublicKeyCallback must be nil")
	}
	session.confCopy.PublicKeyCallback = func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
		authenticated, err := session.client.AuthenticatePublicKey(conn, key)
		if err != nil {
			return nil, err
		}
		if authenticated {
			// store the marshalled public key in the permissions extensions map so we can maintain
			// the state of which publickey actually authenticated for future permission checks
			perms := &ssh.Permissions{}
			perms.Extensions = map[string]string{}
			perms.Extensions[authenticatedPublicKeyExt] = base64.StdEncoding.EncodeToString(key.Marshal())
			return perms, nil
		} else {
			return nil, errors.New("Access denied for given public key.")
		}
	}

	sshConn, chans, reqs, err := ssh.NewServerConn(session.conn, &session.confCopy)
	if err != nil {
		log.Println("Failed to handshake:", err)
		return
	}

	// this must exist because we had to authenticate using the above PublicKeyCallback, and it always
	// provides this key in the resulting Permissions.Extensions object
	pubKeyBytes, err := base64.StdEncoding.DecodeString(sshConn.Permissions.Extensions[authenticatedPublicKeyExt])
	if err != nil {
		log.Println("Could not decode pubkey:", err)
		return
	}
	session.pubKey, err = ssh.ParsePublicKey(pubKeyBytes)
	if err != nil {
		log.Println("Failed to retrieve authenticated pubkey:", err)
		return
	}

	session.client.PublicKeyChosen(session.pubKey)

	// "The Request and NewChannel channels must be serviced, or the connection will hang."
	// https://godoc.org/golang.org/x/crypto/ssh#NewServerConn
	go ssh.DiscardRequests(reqs)

	for ch := range chans {
		if ch.ChannelType() != "session" {
			ch.Reject(ssh.UnknownChannelType, "unknown channel type")
			continue
		}
		go session.handleSSHSessionChannel(sshConn, ch)
	}

}

func (session *ClientSession) handleSSHSessionChannel(conn *ssh.ServerConn, newChan ssh.NewChannel) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		log.Println("newChan.Accept failed:", err)
		return
	}
	defer ch.Close()

	for req := range reqs {
		switch req.Type {
		case "exec":
			session.handleSSHExec(conn, ch, req)
			return
		case "env":
			if req.WantReply {
				req.Reply(true, nil)
			}
		default:
			if req.WantReply {
				req.Reply(false, nil)
			}
		}
	}
}

func (session *ClientSession) handleSSHExec(conn *ssh.ServerConn, ch ssh.Channel, req *ssh.Request) {
	packSession, err := session.setupPackSessionFromReq(req)

	if err != nil {
		log.Println("Error setting up pack session from request: ", err)
		ch.Stderr().Write([]byte("Invalid request.\n"))

		if req.WantReply {
			req.Reply(true, nil)
		}

		return
	}

	if req.WantReply {
		req.Reply(true, nil)
	}

	packSession.HandleGitReceivePack(ch, ch)

	status := struct{ Status uint32 }{0}
	_, err = ch.SendRequest("exit-status", false, ssh.Marshal(&status))
	if err != nil {
		log.Println("ch.SendRequest failed:", err)
		return
	}
}

func (session *ClientSession) setupPackSessionFromReq(req *ssh.Request) (*GitReceiveSession, error) {
	if len(req.Payload) < 4 {
		return nil, errors.New("Payload too short")
	}

	execLen, err := parseInt32(req.Payload[:4])
	if err != nil {
		return nil, fmt.Errorf("Could not parse payload: %s", err.Error())
	}
	if len(req.Payload) != 4+int(execLen) {
		return nil, errors.New("Payload size does not match length field")
	}

	execCmd := string(req.Payload[4:])

	cmdParts := strings.SplitN(execCmd, " ", 2)
	if len(cmdParts) != 2 {
		return nil, errors.New("Expected execution of a git command with a repository as argument")
	}

	cmd := cmdParts[0]
	repoPath := strings.Trim(cmdParts[1], "'")

	if cmd != "git-receive-pack" {
		return nil, errors.New("Expected 'git-receive-pack' as the command to execute.")
	}

	packSession := NewGitReceiveSession()
	packSession.BackingStore, err = session.client.GetRepositoryBackingStore(repoPath)
	if err != nil {
		return nil, errors.New("Error creating internal backing store")
	}

	return packSession, nil
}

func parseInt32(data []byte) (int32, error) {
	var val int32
	buf := bytes.NewReader(data)
	err := binary.Read(buf, binary.BigEndian, &val)
	return val, err
}
