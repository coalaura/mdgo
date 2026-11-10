package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"unsafe"
)

const (
	manifestMagic      = "MDA1"
	manifestHeaderSize = 8
	manifestEntrySize  = 26 // Fixed fields, excluding the name.
)

var ErrInvalidManifest = errors.New("invalid asset manifest")

type ManifestWriter struct {
	buf   []byte
	names map[string]struct{}
}

type Manifest struct {
	buf []byte // immutable
	mp  map[string]ManifestEntry
}

type ManifestEntry struct {
	Name      string
	Hash      uint64
	Size      int64
	ModTimeNS int64
}

func (w *ManifestWriter) Add(entry ManifestEntry) error {
	if !validManifestName(entry.Name) || len(entry.Name) > 1<<16-1 {
		return fmt.Errorf("%w: invalid name %q", ErrInvalidManifest, entry.Name)
	}

	if entry.Size < 0 {
		return fmt.Errorf("%w: negative size for %q", ErrInvalidManifest, entry.Name)
	}

	_, exists := w.names[entry.Name]
	if exists {
		return fmt.Errorf("%w: duplicate name %q", ErrInvalidManifest, entry.Name)
	}

	if uint64(len(w.names)) == 1<<32-1 {
		return fmt.Errorf("%w: too many entries", ErrInvalidManifest)
	}

	if len(w.buf) == 0 {
		w.buf = append(w.buf, manifestMagic...)
		w.buf = binary.LittleEndian.AppendUint32(w.buf, 0)
	}

	if w.names == nil {
		w.names = make(map[string]struct{})
	}

	w.buf = binary.LittleEndian.AppendUint64(w.buf, entry.Hash)
	w.buf = binary.LittleEndian.AppendUint64(w.buf, uint64(entry.Size))
	w.buf = binary.LittleEndian.AppendUint64(w.buf, uint64(entry.ModTimeNS))
	w.buf = binary.LittleEndian.AppendUint16(w.buf, uint16(len(entry.Name)))
	w.buf = append(w.buf, entry.Name...)

	w.names[entry.Name] = struct{}{}

	binary.LittleEndian.PutUint32(w.buf[4:8], uint32(len(w.names)))

	return nil
}

func (w *ManifestWriter) Len() int {
	return len(w.names)
}

func (w *ManifestWriter) WriteTo(dst io.Writer) (int64, error) {
	if len(w.buf) == 0 {
		w.buf = append(w.buf, manifestMagic...)
		w.buf = binary.LittleEndian.AppendUint32(w.buf, 0)
	}

	n, err := dst.Write(w.buf)
	if err == nil && n != len(w.buf) {
		err = io.ErrShortWrite
	}

	return int64(n), err
}

func (w *ManifestWriter) Reset() {
	w.buf = w.buf[:0]

	clear(w.names)
}

func (m *Manifest) Get(name string) (ManifestEntry, bool) {
	entry, ok := m.mp[name]
	return entry, ok
}

func (m *Manifest) Len() int {
	return len(m.mp)
}

func (m *Manifest) Range(yield func(ManifestEntry) bool) {
	for _, entry := range m.mp {
		if !yield(entry) {
			return
		}
	}
}

func (e ManifestEntry) MatchesMetadata(info fs.FileInfo) bool {
	if e.ModTimeNS == 0 || !info.Mode().IsRegular() || e.Size != info.Size() {
		return false
	}

	mtime := info.ModTime()
	return !mtime.IsZero() && e.ModTimeNS == mtime.UnixNano()
}

func ReadManifest(src io.Reader) (*Manifest, error) {
	buf, err := io.ReadAll(src)
	if err != nil {
		return nil, fmt.Errorf("read asset manifest: %w", err)
	}

	return ParseManifest(buf)
}

func ParseManifest(buf []byte) (*Manifest, error) {
	if len(buf) < manifestHeaderSize {
		return nil, fmt.Errorf("%w: truncated header", ErrInvalidManifest)
	}

	if string(buf[:4]) != manifestMagic {
		return nil, fmt.Errorf("%w: unsupported magic or version", ErrInvalidManifest)
	}

	count := binary.LittleEndian.Uint32(buf[4:8])
	if uint64(count) > uint64((len(buf)-manifestHeaderSize)/(manifestEntrySize+1)) {
		return nil, fmt.Errorf("%w: entry count exceeds available data", ErrInvalidManifest)
	}

	m := &Manifest{buf: buf, mp: make(map[string]ManifestEntry, int(count))}

	offset := manifestHeaderSize

	for i := range count {
		if len(buf)-offset < manifestEntrySize {
			return nil, fmt.Errorf("%w: truncated entry %d", ErrInvalidManifest, i)
		}

		fields := buf[offset : offset+manifestEntrySize]

		entry := ManifestEntry{
			Hash:      binary.LittleEndian.Uint64(fields[0:8]),
			Size:      int64(binary.LittleEndian.Uint64(fields[8:16])),
			ModTimeNS: int64(binary.LittleEndian.Uint64(fields[16:24])),
		}

		nameLen := int(binary.LittleEndian.Uint16(fields[24:26]))
		offset += manifestEntrySize

		if nameLen == 0 || nameLen > len(buf)-offset {
			return nil, fmt.Errorf("%w: invalid name length in entry %d", ErrInvalidManifest, i)
		}

		entry.Name = unsafe.String(&buf[offset], nameLen)
		offset += nameLen

		if !validManifestName(entry.Name) || entry.Size < 0 {
			return nil, fmt.Errorf("%w: invalid entry %d", ErrInvalidManifest, i)
		}

		_, exists := m.mp[entry.Name]
		if exists {
			return nil, fmt.Errorf("%w: duplicate name %q", ErrInvalidManifest, entry.Name)
		}

		m.mp[entry.Name] = entry
	}

	if offset != len(buf) {
		return nil, fmt.Errorf("%w: trailing data", ErrInvalidManifest)
	}

	return m, nil
}

func validManifestName(name string) bool {
	return name != "." && name != ".manifest" && !strings.HasPrefix(name, ".manifest/") && fs.ValidPath(name) && !strings.ContainsAny(name, "\\:\x00")
}
