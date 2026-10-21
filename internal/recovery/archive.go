package recovery

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"io"

	"golang.org/x/crypto/scrypt"
)

const (
	archiveMagic = "HBRBK002"
	recordBytes  = 1 << 20
	headerBytes  = len(archiveMagic) + 32 + 4
)

var errArchive = errors.New("invalid, incomplete, or unauthenticated encrypted backup")

type encryptedWriter struct {
	w      io.Writer
	aead   cipher.AEAD
	header []byte
	seq    uint64
	buf    []byte
}

func archiveCipher(password, salt []byte) (cipher.AEAD, error) {
	if len(password) < 16 || len(password) > 4096 {
		return nil, errors.New("backup password must contain 16 to 4096 bytes")
	}
	key, err := scrypt.Key(password, salt, 32768, 8, 1, 32)
	if err != nil {
		return nil, err
	}
	defer clear(key)
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func newEncryptedWriter(w io.Writer, password []byte) (*encryptedWriter, error) {
	header := make([]byte, headerBytes)
	copy(header, archiveMagic)
	if _, err := io.ReadFull(rand.Reader, header[len(archiveMagic):]); err != nil {
		return nil, err
	}
	aead, err := archiveCipher(password, header[8:40])
	if err != nil {
		return nil, err
	}
	if n, writeErr := w.Write(header); writeErr != nil || n != len(header) {
		if writeErr == nil {
			writeErr = io.ErrShortWrite
		}
		return nil, writeErr
	}
	return &encryptedWriter{w: w, aead: aead, header: header, buf: make([]byte, 0, recordBytes)}, nil
}

func recordIdentity(header []byte, seq uint64, length uint32) ([]byte, []byte) {
	nonce := make([]byte, 12)
	copy(nonce, header[40:44])
	binary.BigEndian.PutUint64(nonce[4:], seq)
	aad := make([]byte, headerBytes+12)
	copy(aad, header)
	binary.BigEndian.PutUint64(aad[headerBytes:], seq)
	binary.BigEndian.PutUint32(aad[headerBytes+8:], length)
	return nonce, aad
}

func (w *encryptedWriter) record(kind byte, payload []byte) error {
	// A fresh salt derives a different key per archive; monotonic record nonces
	// authenticate order, including the final record that detects truncation.
	if w.seq == ^uint64(0) {
		return errArchive
	}
	plain := make([]byte, 1+len(payload))
	plain[0] = kind
	copy(plain[1:], payload)
	length := uint32(len(plain) + w.aead.Overhead())
	nonce, aad := recordIdentity(w.header, w.seq, length)
	sealed := w.aead.Seal(plain[:0], nonce, plain, aad)
	var size [4]byte
	binary.BigEndian.PutUint32(size[:], length)
	if n, err := w.w.Write(size[:]); err != nil || n != len(size) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	if n, err := w.w.Write(sealed); err != nil || n != len(sealed) {
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	w.seq++
	return nil
}

func (w *encryptedWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		space := min(recordBytes-len(w.buf), len(p))
		w.buf = append(w.buf, p[:space]...)
		p = p[space:]
		if len(w.buf) == recordBytes {
			if err := w.record(1, w.buf); err != nil {
				return n - len(p), err
			}
			clear(w.buf)
			w.buf = w.buf[:0]
		}
	}
	return n, nil
}

func (w *encryptedWriter) Close() error {
	defer clear(w.buf)
	if len(w.buf) > 0 {
		if err := w.record(1, w.buf); err != nil {
			return err
		}
	}
	return w.record(0, nil)
}

type encryptedReader struct {
	r      io.Reader
	aead   cipher.AEAD
	header []byte
	seq    uint64
	buf    []byte
	done   bool
}

func newEncryptedReader(r io.Reader, password []byte) (*encryptedReader, error) {
	header := make([]byte, headerBytes)
	if _, err := io.ReadFull(r, header); err != nil || string(header[:8]) != archiveMagic {
		return nil, errArchive
	}
	aead, err := archiveCipher(password, header[8:40])
	if err != nil {
		return nil, err
	}
	return &encryptedReader{r: r, aead: aead, header: header}, nil
}

func (r *encryptedReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if len(r.buf) > 0 {
		n := copy(p, r.buf)
		clear(r.buf[:n])
		r.buf = r.buf[n:]
		return n, nil
	}
	if r.done {
		return 0, io.EOF
	}
	var size [4]byte
	if _, err := io.ReadFull(r.r, size[:]); err != nil {
		return 0, errArchive
	}
	length := binary.BigEndian.Uint32(size[:])
	if length < 17 || length > recordBytes+17 || r.seq == ^uint64(0) {
		return 0, errArchive
	}
	sealed := make([]byte, int(length))
	if _, err := io.ReadFull(r.r, sealed); err != nil {
		return 0, errArchive
	}
	nonce, aad := recordIdentity(r.header, r.seq, length)
	plain, err := r.aead.Open(sealed[:0], nonce, sealed, aad)
	if err != nil {
		return 0, errArchive
	}
	r.seq++
	switch plain[0] {
	case 0:
		if len(plain) != 1 {
			return 0, errArchive
		}
		var trailing [1]byte
		if n, err := r.r.Read(trailing[:]); n != 0 || err != io.EOF {
			return 0, errArchive
		}
		r.done = true
		return 0, io.EOF
	case 1:
		if len(plain) < 2 {
			return 0, errArchive
		}
		r.buf = plain[1:]
		return r.Read(p)
	default:
		return 0, errArchive
	}
}
