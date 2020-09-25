package vkern

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"golang.org/x/crypto/openpgp"
)

// Global ..
var Global Manager

var prefix = "kernel-"

func versionFromFilename(s string) (CalVer, error) {
	if !strings.HasPrefix(s, prefix) {
		return "", errors.New("bad kernel file name")
	}
	s = strings.TrimPrefix(s, prefix)
	s = filepath.Base(s)
	return Parse(s)
}

func filenameFromVersion(v CalVer) string {
	return prefix + v.String()
}

// Logger ..
var Logger = func(fmt string, x ...interface{}) {
	return
}

// ManagedBundle ..
type ManagedBundle struct {
	bundle   *Bundle
	closer   io.Closer
	location string
}

// Bundle ..
func (bundle *ManagedBundle) Bundle() *Bundle {
	return bundle.bundle
}

// Close ..
func (bundle *ManagedBundle) Close() error {
	return bundle.closer.Close()
}

// Location ..
func (bundle *ManagedBundle) Location() string {
	return bundle.location
}

// Get ..
func (mgr *CompoundManager) Get(ctx context.Context, version CalVer) (*ManagedBundle, error) {
	var tuple *Tuple

	// Get BestMatch
	list, err := mgr.List(ctx)
	if err == nil {
		tuple, err = list.BestMatch(version)
	}

	if err != nil {
		return nil, err
	}

	b, err := mgr.mgrs[tuple.Idx].Get(ctx, tuple.Version)
	if err != nil {
		return nil, err
	}

	return b, nil
}

// Tuple ..
type Tuple struct {
	Idx      int
	Version  CalVer    `json:"version"`
	Location string    `json:"source"`
	ModTime  time.Time `json:"release"`
}

// List ..
type List []Tuple

func (l List) Len() int {
	return len(l)
}

func (l List) Less(i, j int) bool {
	if l[i].Version.Less(l[j].Version) {
		return true
	}
	if !l[j].Version.Less(l[i].Version) {
		// two identical versions, so we go based off ModTime
		return l[i].ModTime.Before(l[j].ModTime)
	}
	return false
}

func (l List) Swap(i, j int) {
	tmp := l[i]
	l[i] = l[j]
	l[j] = tmp
}

// BestMatch ..
func (l List) BestMatch(v CalVer) (*Tuple, error) {
	Idx := sort.Search(l.Len(), func(arg1 int) bool {
		return v.LessEq(l[arg1].Version)
	})

	if Idx < l.Len() && l[Idx].Version.String() == v.String() {
		// check later versions since duplicate versions might exist in compound source lists
		for {
			Idx++
			if Idx >= l.Len() || l[Idx].Version.String() != v.String() {
				Idx--
				return &l[Idx], nil // exact match
			}
		}
	}
	// if v has a modifier it demands an exact match
	if v.Modifier() != "" {
		return nil, fmt.Errorf("no match for kernel %s", v.String())
	}

	var candidate *Tuple
	var cErr error = fmt.Errorf("no match for kernel %s", v.String()) // Place holder error for if there is no valid candidate
	if Idx != 0 {
		potentialCandidate := &l[Idx-1]
		if potentialCandidate.Version.Major() == v.Major() &&
			potentialCandidate.Version.Minor() == v.Minor() {
			if v.Patch() == -1 || potentialCandidate.Version.Patch() == v.Patch() {
				// Candidate found, set error to nil
				candidate = potentialCandidate
				cErr = nil
			}
		}
	}

	return candidate, cErr

}

// Manager ..
type Manager interface {
	Get(ctx context.Context, version CalVer) (*ManagedBundle, error)
	List(ctx context.Context) (List, error)
	Latest() (string, error)
}

// Utils
func removeFiles(files ...string) error {
	for _, fPath := range files {
		err := os.Remove(fPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("could not remove %s , error: %v", fPath, err)
		}
	}

	return nil
}

func validateKernelSignature(kernelFile string, signatureFile string) error {
	krrData := mustGetAsset("vorteil.gpg")
	krr := bytes.NewReader(krrData)

	ker, err := os.Open(kernelFile)
	if err != nil {
		return err
	}
	defer ker.Close()

	sig, err := os.Open(signatureFile)
	if err != nil {
		return err
	}
	defer sig.Close()

	kr, err := openpgp.ReadArmoredKeyRing(krr)
	if err != nil {
		return err
	}

	_, err = openpgp.CheckArmoredDetachedSignature(kr, ker, sig)
	if err == nil {
		err = ker.Close()
		if err == nil {
			err = sig.Close()
		}
	}

	return err
}

// AdvancedArgs ..
type AdvancedArgs struct {
	Directory          string   `toml:"directory"`
	DropPath           string   `toml:"drop-path"`
	RemoteRepositories []string `toml:"remote-repositories"`
	// UpgradeStrategy    string   `toml:"upgrade-strategy"`
}

// Advanced ..
func Advanced(args AdvancedArgs) (Manager, error) {

	var mgrs []Manager

	if args.DropPath != "" {
		m, err := NewLocalManager(args.DropPath)
		if err != nil {
			return nil, err
		}
		mgrs = append(mgrs, m)
	}

	for _, s := range args.RemoteRepositories {
		m, err := NewRemoteManager(s, filepath.Join(args.Directory, strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://"), "/", "_")))
		if err != nil {
			return nil, err
		}
		mgrs = append(mgrs, m)
	}

	return NewCompoundManager(mgrs...)
}
