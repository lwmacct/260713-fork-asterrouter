package controlplane

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type ArtifactByteRange struct {
	Offset int64
	Length int64
}

type ArtifactRead struct {
	Body       io.ReadCloser
	Offset     int64
	SizeBytes  int64
	TotalBytes int64
}

type ArtifactStore interface {
	Driver() string
	Put(ctx context.Context, key string, body io.Reader, sizeBytes int64, mediaType string) (int64, error)
	Open(ctx context.Context, key string, byteRange *ArtifactByteRange) (ArtifactRead, error)
	Delete(ctx context.Context, key string) error
}

type MemoryArtifactStore struct {
	mu      sync.RWMutex
	objects map[string][]byte
}

func NewMemoryArtifactStore() *MemoryArtifactStore {
	return &MemoryArtifactStore{objects: map[string][]byte{}}
}

func (*MemoryArtifactStore) Driver() string { return ArtifactStoreDriverMemory }

func (s *MemoryArtifactStore) Put(ctx context.Context, key string, body io.Reader, sizeBytes int64, _ string) (int64, error) {
	if err := validateArtifactStoreWrite(key, body, sizeBytes); err != nil {
		return 0, err
	}
	data, err := io.ReadAll(&contextReader{ctx: ctx, reader: body})
	if err != nil {
		return 0, err
	}
	if sizeBytes >= 0 && int64(len(data)) != sizeBytes {
		return 0, ErrArtifactIntegrity
	}
	s.mu.Lock()
	s.objects[key] = append([]byte(nil), data...)
	s.mu.Unlock()
	return int64(len(data)), nil
}

func (s *MemoryArtifactStore) Open(_ context.Context, key string, byteRange *ArtifactByteRange) (ArtifactRead, error) {
	if !validArtifactStoreKey(key) {
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	s.mu.RLock()
	data, found := s.objects[key]
	data = append([]byte(nil), data...)
	s.mu.RUnlock()
	if !found {
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	offset, length, err := normalizeArtifactByteRange(int64(len(data)), byteRange)
	if err != nil {
		return ArtifactRead{}, err
	}
	return ArtifactRead{
		Body: io.NopCloser(bytes.NewReader(data[offset : offset+length])), Offset: offset,
		SizeBytes: length, TotalBytes: int64(len(data)),
	}, nil
}

func (s *MemoryArtifactStore) Delete(_ context.Context, key string) error {
	if !validArtifactStoreKey(key) {
		return ErrArtifactUnavailable
	}
	s.mu.Lock()
	delete(s.objects, key)
	s.mu.Unlock()
	return nil
}

type LocalArtifactStore struct {
	root string
}

func NewLocalArtifactStore(root string) (*LocalArtifactStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("local artifact root is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve local artifact root: %w", err)
	}
	if err := os.MkdirAll(absolute, 0750); err != nil {
		return nil, fmt.Errorf("create local artifact root: %w", err)
	}
	return &LocalArtifactStore{root: absolute}, nil
}

func (*LocalArtifactStore) Driver() string { return ArtifactStoreDriverLocal }

func (s *LocalArtifactStore) Put(ctx context.Context, key string, body io.Reader, sizeBytes int64, _ string) (written int64, err error) {
	if err := validateArtifactStoreWrite(key, body, sizeBytes); err != nil {
		return 0, err
	}
	target := s.objectPath(key)
	if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
		return 0, fmt.Errorf("create artifact directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(target), ".artifact-*")
	if err != nil {
		return 0, fmt.Errorf("create artifact temporary file: %w", err)
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		if err != nil {
			_ = os.Remove(temporaryName)
		}
	}()
	if err = temporary.Chmod(0600); err != nil {
		return 0, fmt.Errorf("secure artifact temporary file: %w", err)
	}
	written, err = io.Copy(temporary, &contextReader{ctx: ctx, reader: body})
	if err != nil {
		return 0, fmt.Errorf("write local artifact: %w", err)
	}
	if sizeBytes >= 0 && written != sizeBytes {
		return 0, ErrArtifactIntegrity
	}
	if err = temporary.Sync(); err != nil {
		return 0, fmt.Errorf("sync local artifact: %w", err)
	}
	if err = temporary.Close(); err != nil {
		return 0, fmt.Errorf("close local artifact: %w", err)
	}
	if err = os.Rename(temporaryName, target); err != nil {
		return 0, fmt.Errorf("commit local artifact: %w", err)
	}
	return written, nil
}

func (s *LocalArtifactStore) Open(_ context.Context, key string, byteRange *ArtifactByteRange) (ArtifactRead, error) {
	if !validArtifactStoreKey(key) {
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	file, err := os.Open(s.objectPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	if err != nil {
		return ArtifactRead{}, fmt.Errorf("open local artifact: %w", err)
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return ArtifactRead{}, fmt.Errorf("stat local artifact: %w", err)
	}
	offset, length, err := normalizeArtifactByteRange(info.Size(), byteRange)
	if err != nil {
		_ = file.Close()
		return ArtifactRead{}, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		_ = file.Close()
		return ArtifactRead{}, fmt.Errorf("seek local artifact: %w", err)
	}
	return ArtifactRead{Body: &limitedReadCloser{Reader: io.LimitReader(file, length), closer: file}, Offset: offset, SizeBytes: length, TotalBytes: info.Size()}, nil
}

func (s *LocalArtifactStore) Delete(_ context.Context, key string) error {
	if !validArtifactStoreKey(key) {
		return ErrArtifactUnavailable
	}
	err := os.Remove(s.objectPath(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("delete local artifact: %w", err)
	}
	return nil
}

func (s *LocalArtifactStore) objectPath(key string) string {
	return filepath.Join(s.root, filepath.FromSlash(key))
}

func validateArtifactStoreWrite(key string, body io.Reader, sizeBytes int64) error {
	if !validArtifactStoreKey(key) || body == nil || sizeBytes < -1 {
		return errors.New("invalid artifact store write")
	}
	return nil
}

func validArtifactStoreKey(key string) bool {
	if key == "" || strings.HasPrefix(key, "/") || strings.ContainsAny(key, "\x00\r\n\\") {
		return false
	}
	cleaned := path.Clean(key)
	return cleaned == key && cleaned != "." && cleaned != ".." && !strings.HasPrefix(cleaned, "../")
}

func normalizeArtifactByteRange(total int64, requested *ArtifactByteRange) (int64, int64, error) {
	if total < 0 {
		return 0, 0, ErrArtifactUnavailable
	}
	if requested == nil {
		return 0, total, nil
	}
	if requested.Offset < 0 || requested.Offset >= total || requested.Length < 0 {
		return 0, 0, ErrArtifactUnavailable
	}
	length := requested.Length
	if length == 0 || length > total-requested.Offset {
		length = total - requested.Offset
	}
	return requested.Offset, length, nil
}

type limitedReadCloser struct {
	io.Reader
	closer io.Closer
}

func (r *limitedReadCloser) Close() error { return r.closer.Close() }

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(buffer []byte) (int, error) {
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	default:
		return r.reader.Read(buffer)
	}
}
