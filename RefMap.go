package gitpacklib

import (
	"encoding/json"
)

type RefMap struct {
	Refs map[string]string
}

func NewRefMap() *RefMap {
	rm := &RefMap{}
	rm.Refs = make(map[string]string)
	return rm
}

func (r *RefMap) Serialize() []byte {
	json, _ := json.Marshal(r)
	return json
}

func (r *RefMap) Deserialize(buf []byte) {
	json.Unmarshal(buf, r)
}

func (r *RefMap) Get(name string) string {
	return r.Refs[name]
}

func (r *RefMap) Set(name string, value string) {
	r.Refs[name] = value
}

func (r *RefMap) Length() int {
	return len(r.Refs)
}
