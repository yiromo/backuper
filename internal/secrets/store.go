// Package secrets manages an age-encrypted key-value store of passwords.
package secrets

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"filippo.io/age"
)

type Store interface {
	Get(ref string) (string, error)
	Set(ref, value string) error
	Delete(ref string) error
	List() ([]string, error)
}

func DefaultStorePath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "backuper", "secrets.age")
}

type AgeStore struct {
	path       string
	passphrase string
	data       map[string]string
}

func NewAgeStore(path, passphrase string) (*AgeStore, error) {
	s := &AgeStore{
		path:       path,
		passphrase: passphrase,
		data:       make(map[string]string),
	}
	if err := s.load(); err != nil {
		if os.IsNotExist(err) {
			return s, nil // new store — start empty
		}
		return nil, err
	}
	return s, nil
}

func (s *AgeStore) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer f.Close()

	id, err := age.NewScryptIdentity(s.passphrase)
	if err != nil {
		return fmt.Errorf("creating age identity: %w", err)
	}
	r, err := age.Decrypt(f, id)
	if err != nil {
		return fmt.Errorf("decrypting secrets file: %w", err)
	}
	if err := json.NewDecoder(r).Decode(&s.data); err != nil {
		return fmt.Errorf("parsing secrets: %w", err)
	}
	return nil
}

func (s *AgeStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return fmt.Errorf("creating secrets dir: %w", err)
	}
	f, err := os.OpenFile(s.path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("opening secrets file: %w", err)
	}
	defer f.Close()

	rec, err := age.NewScryptRecipient(s.passphrase)
	if err != nil {
		return fmt.Errorf("creating age recipient: %w", err)
	}
	w, err := age.Encrypt(f, rec)
	if err != nil {
		return fmt.Errorf("encrypting secrets: %w", err)
	}
	if err := json.NewEncoder(w).Encode(s.data); err != nil {
		w.Close()
		return fmt.Errorf("encoding secrets: %w", err)
	}
	return w.Close()
}

func (s *AgeStore) Get(ref string) (string, error) {
	v, ok := s.data[ref]
	if !ok {
		return "", fmt.Errorf("secret %q not found in store", ref)
	}
	return v, nil
}

func (s *AgeStore) Set(ref, value string) error {
	s.data[ref] = value
	return s.save()
}

func (s *AgeStore) Delete(ref string) error {
	if _, ok := s.data[ref]; !ok {
		return fmt.Errorf("secret %q not found", ref)
	}
	delete(s.data, ref)
	return s.save()
}

func (s *AgeStore) List() ([]string, error) {
	keys := make([]string, 0, len(s.data))
	for k := range s.data {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys, nil
}

func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
