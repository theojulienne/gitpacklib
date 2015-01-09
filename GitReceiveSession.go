package gitpacklib

import (
	"bufio"
	"bytes"
	"compress/zlib"
	"crypto/sha1"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
)

const RefsKey = "refs"

type GitReceiveSession struct {
	BackingStore BackingStore
	refMap       *RefMap

	updatedRefs []string

	gitVersion int32
}

func NewGitReceiveSession() *GitReceiveSession {
	session := &GitReceiveSession{}
	return session
}

func (session *GitReceiveSession) HandleGitReceivePack(in_ io.Reader, out io.Writer) {
	session.refMap = NewRefMap()

	session.BackingStore.Lock()
	defer session.BackingStore.Unlock()

	// load the existing refs (if any) from the backing store
	refMapBytes, err := session.BackingStore.Get(RefsKey)
	if err == nil {
		session.refMap.Deserialize(refMapBytes)
	}

	capabilitySuffix := "\x00report-status delete-refs agent=gitpacklib/0.0.0"

	if session.refMap.Length() == 0 {
		writeGitMessage(out, "0000000000000000000000000000000000000000 capabilities^{}"+capabilitySuffix)
	} else {
		for k, v := range session.refMap.Refs {
			writeGitMessage(out, v+" "+k+capabilitySuffix)
			capabilitySuffix = ""
		}
	}
	terminateGitMessages(out)

	in := bufio.NewReader(in_)
	pushedRefs := false

	for {
		sizeHex, err := in.Peek(4)
		size, err := strconv.ParseInt(string(sizeHex), 16, 16)

		if size == 0 {
			// log.Println("Got 0000 line")
			buf := make([]byte, 4)
			io.ReadFull(in, buf)
			break
		}

		buf := make([]byte, size)
		n, err := io.ReadFull(in, buf)

		if err != nil || n == 0 {
			break
		}

		// log.Println("Read", n, err, string(buf))

		// firstly, strip off any capabiltiies (\x00 and thereafter)
		line := string(buf)
		caps := strings.Split(line, "\x00")
		refLine := caps[0]

		// then work split up the ref details
		refParts := strings.Split(refLine, " ")
		if len(refParts) == 3 {
			//oldSha := refParts[0]
			newSha := refParts[1]
			ref := refParts[2]

			session.refMap.Set(ref, newSha)
			session.updatedRefs = append(session.updatedRefs, ref)
			pushedRefs = true
		}
	}

	// now we expect the PACK containing the new data
	if pushedRefs {
		sizeHex, err := in.Peek(4)
		if string(sizeHex) != "PACK" {
			writeGitMessage(out, "unpack invalid header")
			return
		}

		err = session.handleGitUnpackStream(in)
		if err == nil {
			writeGitMessage(out, "unpack ok")
		} else {
			log.Println("Error during unpack:", err.Error())
			writeGitMessage(out, "unpack "+err.Error())
			terminateGitMessages(out)
			return
		}
	}

	// save the refs to the store now that we're done
	refMapBytes = session.refMap.Serialize()
	err = session.BackingStore.Set(RefsKey, refMapBytes)
	globalOk := "ok"
	if err != nil {
		globalOk = "ng"
	}

	// FIXME: check that the commits relating to these refs were correctly added
	// and that all their trees, blobs, etc were all also valid, and that the old
	// ref was part of that history (if we care about that ?)
	for _, ref := range session.updatedRefs {
		writeGitMessage(out, globalOk+" "+ref)
	}

	terminateGitMessages(out)
}

func (session *GitReceiveSession) handleGitUnpackStream(rawStream *bufio.Reader) error {
	stream := NewSHA1Reader(rawStream)

	hdr := make([]byte, 12)
	_, err := io.ReadFull(stream, hdr)
	if err != nil {
		return errors.New("Error reading PACK header: " + err.Error())
	}

	if string(hdr[:4]) != "PACK" {
		return errors.New("Invalid PACK header")
	}

	var gitVersion int32
	var numObjects int32
	hdrbuf := bytes.NewReader(hdr[4:])
	err = binary.Read(hdrbuf, binary.BigEndian, &gitVersion)
	err = binary.Read(hdrbuf, binary.BigEndian, &numObjects)
	if err != nil {
		return errors.New("Error parsing header: " + err.Error())
	}

	session.gitVersion = gitVersion
	log.Println("Client requested git PACK version", gitVersion)

	// fmt.Println("Num objects:", numObjects)
	err = session.receivePackObjects(numObjects, stream)
	if err != nil {
		return err
	}

	computedSha := stream.Sum(nil)
	receivedSha := make([]byte, sha1.Size)
	_, err = io.ReadFull(stream, receivedSha)
	if err != nil {
		return errors.New("Error reading object sha: " + err.Error())
	}

	if !bytes.Equal(computedSha[:], receivedSha) {
		return errors.New(fmt.Sprintf("sha1 sum mismatch: %s != %s!", computedSha, receivedSha))
	}

	// log.Println("Got hash and now done with receiving pack")

	return nil
}

func (session *GitReceiveSession) receivePackObjects(numObjects int32, stream *HashingReader) error {
	for i := 0; i < int(numObjects); i++ {
		c, err := stream.ReadByte()
		if err != nil {
			return errors.New("Error reading object: " + err.Error())
		}

		objType := (c >> 4) & 0x7
		var objLength int64 = int64(c & 0xf)
		var lenBits uint
		lenBits = 4
		for (c & 0x80) != 0 {
			// log.Println("len=",objLength, "c=",c)
			c, err = stream.ReadByte()
			if err != nil {
				return errors.New("Error reading object: " + err.Error())
			}

			objLength |= int64(c&0x7f) << lenBits
			lenBits += 7
		}
		// log.Println("len=",objLength, "c=",c)

		// fmt.Printf("Got type=%d len=%d\n", objType, objLength)

		if objType == 6 {
			return errors.New("base offset deltas not supported")
		}

		var originalSha string
		if objType == 7 {
			originalShaBytes := make([]byte, sha1.Size)
			_, err = io.ReadFull(stream, originalShaBytes)
			if err != nil {
				return errors.New("Error reading ID of delta base object: " + err.Error())
			}
			originalSha = hex.EncodeToString(originalShaBytes)
		}

		inflated, err := zlib.NewReader(stream)
		if err != nil {
			panic(err)
		}

		obj := make([]byte, objLength)
		n, err := io.ReadFull(inflated, obj)
		if err != nil {
			return errors.New("Error reading object data: " + err.Error())
		}
		if int64(n) != objLength {
			return errors.New("Data truncated!")
		}
		// force a read to clean up
		tmp := make([]byte, 1)
		n, err = inflated.Read(tmp)
		if n != 0 || err != io.EOF {
			return errors.New("Error getting expected EOF and cleanup: " + err.Error())
		}

		inflated.Close()

		typeStr := gitTypeToString(objType)

		if objType == 7 {
			deltaData := obj

			originalType, originalObject, err := session.loadObject(originalSha)
			if err != nil {
				return errors.New("Error loading delta base object by SHA: " + err.Error())
			}

			typeStr = originalType

			// perform the actual delta decoding
			obj, err = session.performDeltaDecode(originalObject, deltaData)
			if err != nil {
				return errors.New("Error rewriting object from delta: " + err.Error())
			}
		}

		// fmt.Println("Got", t, ":", obj)

		_, err = session.saveObject(typeStr, obj)

		if err != nil {
			return errors.New("Error saving object: " + err.Error())
		} else {
			// log.Println("SHA saved:", sha)
		}
	}

	return nil
}

func (session *GitReceiveSession) parseMultiByteInt(stream io.ByteReader) (result int, err error) {
	c, err := stream.ReadByte()
	if err != nil {
		return 0, errors.New("Error reading object: " + err.Error())
	}

	var number int = int(c & 0x7f)
	var shift uint = 7
	for (c & 0x80) != 0 {
		c, err = stream.ReadByte()
		if err != nil {
			return 0, errors.New("Error reading object: " + err.Error())
		}

		number |= int(c&0x7f) << shift
		shift += 7
	}

	return number, nil
}

func (session *GitReceiveSession) performDeltaDecode(base []byte, delta []byte) (computed []byte, err error) {
	// read the delta header
	deltaReader := bytes.NewReader(delta)
	computedObject := &bytes.Buffer{}

	baseObjectLength, err := session.parseMultiByteInt(deltaReader)
	if err != nil {
		return nil, err
	}
	if baseObjectLength != len(base) {
		return nil, errors.New(fmt.Sprintf("Base object length mismatch (%d != %d)", baseObjectLength, len(base)))
	}

	resultObjectLength, err := session.parseMultiByteInt(deltaReader)
	if err != nil {
		return nil, err
	}

	for deltaReader.Len() > 0 {
		c, err := deltaReader.ReadByte()
		if err != nil {
			return nil, errors.New("Error reading delta: " + err.Error())
		}

		if (c & 0x80) == 0 {
			// insert hunk
			numBytesToInsert := int64(c & 0x7f)
			_, err = io.CopyN(computedObject, deltaReader, numBytesToInsert)
			if err != nil {
				return nil, err
			}
		} else {
			// copy hunk
			opcode := c

			var copy_offset int64 = 0
			var copy_length int64 = 0

			var shift uint64 = 0
			var i uint64 = 0

			for shift, i = 0, 0; i < 4; i++ {
				if (opcode & 0x01) != 0 {
					c, err := deltaReader.ReadByte()
					if err != nil {
						return nil, errors.New("Error reading delta: " + err.Error())
					}

					copy_offset |= int64(c) << shift
				}

				opcode >>= 1
				shift += 8
			}

			var lenBits uint64 = 3

			if session.gitVersion == 2 {
				lenBits = 2
			}

			for shift, i = 0, 0; i < lenBits; i++ {
				if (opcode & 0x01) != 0 {
					c, err := deltaReader.ReadByte()
					if err != nil {
						return nil, errors.New("Error reading delta: " + err.Error())
					}

					copy_length |= int64(c) << shift
				}

				opcode >>= 1
				shift += 8
			}

			if copy_length == 0 {
				copy_length = (1 << 16)
			}

			// fmt.Printf("copy_offset=%d, copy_length=%d, sum=%d len=%d\n", copy_offset, copy_length, copy_offset+copy_length, len(base))

			computedObject.Write(base[copy_offset : copy_offset+copy_length])
		}
	}

	b := computedObject.Bytes()

	if len(b) != resultObjectLength {
		return nil, errors.New("Computed delta object length mismatch")
	}

	return b, nil
}

func (session *GitReceiveSession) saveObject(objType string, data []byte) (sha string, err error) {
	h := sha1.New()
	b := new(bytes.Buffer)

	both := io.MultiWriter(h, b)
	both.Write([]byte(fmt.Sprintf("%s %d", objType, len(data))))
	both.Write([]byte("\x00"))
	both.Write(data)

	sha = hex.EncodeToString(h.Sum(nil))

	err = session.BackingStore.Set("object/"+sha, b.Bytes())

	return sha, err
}

func (session *GitReceiveSession) loadObject(sha string) (objType string, data []byte, err error) {
	allContent, err := session.BackingStore.Get("object/" + sha)
	if err != nil {
		return "", nil, err
	}

	parts := bytes.SplitN(allContent, []byte{0}, 2)
	if len(parts) != 2 {
		return "", nil, errors.New("Expected null byte separating content and header")
	}

	var dataSize int
	fmt.Sscanf(string(parts[0]), "%s %d", &objType, &dataSize)

	data = parts[1]

	return objType, data, nil
}

func writeGitMessage(out io.Writer, message string) {
	msgLen := 4 + len(message) + 1
	out.Write([]byte(fmt.Sprintf("%04x", msgLen)))
	out.Write([]byte(message))
	out.Write([]byte("\n"))
}

func terminateGitMessages(out io.Writer) {
	out.Write([]byte("0000"))
}

func gitTypeToString(objType byte) string {
	switch objType {
	case 1:
		return "commit"
	case 2:
		return "tree"
	case 3:
		return "blob"
	case 4:
		return "tag"
	}

	return "unknown"
}
