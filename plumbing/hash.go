package plumbing

import (
	"bytes"
	"crypto"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strconv"

	"github.com/go-git/go-git/v5/plumbing/hash"
)

// Hash is the hashed content.
type Hash struct {
	// Hash is the hashed content.
	Hash [sha256.Size]byte
	// Algo is the hash algorithm used. When Algo is zero, SHA1 is used. This
	// will panic if the hash algorithm is not supported and not either SHA1 or
	// SHA256.
	Algo crypto.Hash
}

// ZeroHash is Hash with value zero
var ZeroHash Hash

// ComputeHash compute the hash for a given ObjectType and content
func ComputeHash(t ObjectType, content []byte) Hash {
	h := NewHasher(t, int64(len(content)))
	h.Write(content)
	return h.Sum()
}

// NewHash return a new Hash from a hexadecimal hash representation
func NewHash(s string) Hash {
	b, _ := hex.DecodeString(s)

	var h Hash
	copy(h.Hash[:], b)
	switch len(b) / 2 {
	case sha1.Size:
		h.Algo = crypto.SHA1
	case sha256.Size:
		h.Algo = crypto.SHA256
	}

	return h
}

var empty Hash

func (h Hash) IsZero() bool {
	return h.Hash == empty.Hash
}

func (h Hash) String() string {
	switch h.Algo {
	case 0, crypto.SHA1:
		return hex.EncodeToString(h.Hash[:sha1.Size])
	case crypto.SHA256:
		return hex.EncodeToString(h.Hash[:sha256.Size])
	}
	panic("unsupported hash algorithm")
}

// Equal returns true if the hashes are equal.
func (h Hash) Equal(o Hash) bool {
	lhsAlgo := h.Algo
	rhsAlgo := o.Algo
	if lhsAlgo == 0 {
		lhsAlgo = crypto.SHA1
	}
	if rhsAlgo == 0 {
		rhsAlgo = crypto.SHA1
	}
	return lhsAlgo == rhsAlgo && h.Hash == o.Hash
}

// Compare returns an integer comparing two hashes lexicographically. The
// result will be 0 if a==b, -1 if a < b, and +1 if a > b.
func (h Hash) Compare(o Hash) int {
	lhsAlgo := h.Algo
	rhsAlgo := o.Algo
	if lhsAlgo == 0 {
		lhsAlgo = crypto.SHA1
	}
	if rhsAlgo == 0 {
		rhsAlgo = crypto.SHA1
	}
	if lhsAlgo != rhsAlgo {
		return int(lhsAlgo) - int(rhsAlgo)
	}
	return bytes.Compare(h.Hash[:], o.Hash[:])
}

type Hasher struct {
	hash.Hash
}

func NewHasher(t ObjectType, size int64) Hasher {
	h := Hasher{hash.New(crypto.SHA1)}
	h.Reset(t, size)
	return h
}

func (h Hasher) Reset(t ObjectType, size int64) {
	h.Hash.Reset()
	h.Write(t.Bytes())
	h.Write([]byte(" "))
	h.Write([]byte(strconv.FormatInt(size, 10)))
	h.Write([]byte{0})
}

func (h Hasher) Sum() (hash Hash) {
	copy(hash.Hash[:], h.Hash.Sum(nil))
	return
}

// HashesSort sorts a slice of Hashes in increasing order.
func HashesSort(a []Hash) {
	sort.Sort(HashSlice(a))
}

// HashSlice attaches the methods of sort.Interface to []Hash, sorting in
// increasing order.
type HashSlice []Hash

func (p HashSlice) Len() int           { return len(p) }
func (p HashSlice) Less(i, j int) bool { return bytes.Compare(p[i].Hash[:], p[j].Hash[:]) < 0 }
func (p HashSlice) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// IsHash returns true if the given string is a valid hash.
func IsHash(s string) bool {
	switch len(s) {
	case sha1.Size * 2, sha256.Size * 2:
		_, err := hex.DecodeString(s)
		return err == nil
	default:
		return false
	}
}
