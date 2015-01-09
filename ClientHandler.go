package gitpacklib

type ClientHandler interface {
	NewClient() Client
}
