package gitpacklib

type BackingStore interface {
	Lock()
	Unlock()

	Set(name string, value []byte) error
	Get(name string) ([]byte, error)
}
