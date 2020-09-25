package vkern

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cavaliercoder/grab"
	"golang.org/x/crypto/openpgp"
	"gopkg.in/yaml.v2"
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
	if Idx != 0 {
		candidate := &l[Idx-1]
		if candidate.Version.Major() == v.Major() &&
			candidate.Version.Minor() == v.Minor() {
			if v.Patch() == -1 || candidate.Version.Patch() == v.Patch() {
				return candidate, nil
			}
		}
	}

	return nil, fmt.Errorf("no match for kernel %s", v.String())

}

// Manager ..
type Manager interface {
	Get(ctx context.Context, version CalVer) (*ManagedBundle, error)
	List(ctx context.Context) (List, error)
	Latest() (string, error)
}

//
// LocalManager
//

// LocalManager ..
type LocalManager struct {
	dir string
}

// NewLocalManager ..
func NewLocalManager(path string) (*LocalManager, error) {
	mgr := new(LocalManager)
	err := os.MkdirAll(path, 0777)
	if err != nil {
		return nil, err
	}
	mgr.dir = path
	return mgr, nil
}

// Get ..
func (mgr *LocalManager) Get(ctx context.Context, version CalVer) (*ManagedBundle, error) {
	var tuple *Tuple

	// Get BestMatch
	list, err := mgr.List(ctx)
	if err == nil {
		tuple, err = list.BestMatch(version)
	}

	if err != nil {
		return nil, err
	}

	path := filepath.Join(mgr.dir, filenameFromVersion(tuple.Version))
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	b := new(ManagedBundle)
	b.bundle, err = NewBundle(f)
	if err != nil {
		f.Close()
	} else {
		b.closer = f
		b.location = path
	}

	return b, err

}

func latest(mgr Manager) (out string, err error) {
	list, err := mgr.List(context.Background())
	if err != nil {
		return
	}

	latest := time.Unix(0, 0)
	for _, l := range list {
		if l.ModTime.Unix() > latest.Unix() {
			latest = l.ModTime
			out = l.Version.String()
		}
	}

	return
}

// Latest ..
func (mgr *LocalManager) Latest() (string, error) {
	return latest(mgr)
}

// Latest ..
func (mgr *CompoundManager) Latest() (string, error) {
	return latest(mgr)
}

// Latest ..
func (mgr *RemoteManager) Latest() (string, error) {
	return latest(mgr)
}

// List ..
func (mgr *LocalManager) List(ctx context.Context) (List, error) {
	fis, err := ioutil.ReadDir(mgr.dir)
	if err != nil {
		return nil, err
	}

	var list List
	for _, fi := range fis {
		v, err := versionFromFilename(fi.Name())
		if err != nil {
			Logger("Kernels directory contains invalid object: %s", filepath.Join(mgr.dir, fi.Name()))
			continue
		}
		list = append(list, Tuple{
			Version:  v,
			Location: mgr.dir,
			ModTime:  fi.ModTime(),
		})
	}

	sort.Sort(list)
	return list, nil
}

// Close ..
func (mgr *LocalManager) Close() error {
	return nil
}

//
// RemoteManager
//

// RemoteManager ..
type RemoteManager struct {
	url    string
	dir    string
	lock   sync.RWMutex
	dlLock sync.Mutex
	closed bool
	ch     chan bool
	cache  List
}

// NewRemoteManager ..
func NewRemoteManager(url, dir string) (*RemoteManager, error) {
	mgr := new(RemoteManager)
	err := os.MkdirAll(dir, 0777)
	if err != nil {
		return nil, err
	}
	mgr.url = url
	mgr.dir = dir
	mgr.ch = make(chan bool)
	// Check if local file
	if _, err := os.Stat(filepath.Join(mgr.dir, "cached-kernel-manifest")); err == nil {
		// Path exists
		f, err := os.Open(filepath.Join(mgr.dir, "cached-kernel-manifest"))
		if err != nil {
			Logger("Unable to read the file for the cached manifest: '%s'", err)
		}
		b, err := ioutil.ReadAll(f)
		if err != nil {
			Logger("Unable to read the remote cached manifest: '%s'", err)
		}
		var list List
		err = json.Unmarshal(b, &list)
		if err != nil {
			Logger("Unable to unmarshal into the struct from a cached file: '%s'", err)
		}
		mgr.cache = list
	}
	go mgr.poll()
	return mgr, nil
}

func (mgr *RemoteManager) pollOnce(ctx context.Context) error {
	list, err := mgr.remoteList(ctx)
	if err != nil {
		Logger("Remote kernels manager for '%s' failed to get remote list: %v", mgr.url, err)
		return err
	}

	mgr.lock.Lock()
	for i, tuple := range list {
		fi, err := os.Stat(filepath.Join(mgr.dir, filenameFromVersion(tuple.Version)))
		if err == nil {
			if tuple.ModTime.Before(fi.ModTime()) {
				list[i].Location += " (cached)"
			} else {
				Logger("Remote kernels manager '%s' detected an unusual remote kernel file update '%s'", mgr.url, tuple.Version)
				go func() {
					err = os.Remove(filepath.Join(mgr.dir, filenameFromVersion(tuple.Version)))
					if err != nil {
						Logger("Issue in '%s' remote kernels manager's cache directory '%s': %v", mgr.url, mgr.dir, err)
					}
				}()
			}
		} else if os.IsNotExist(err) {
			continue
		} else {
			Logger("Issue in '%s' remote kernels manager's cache directory '%s': %v", mgr.url, mgr.dir, err)
		}
	}
	mgr.cache = list

	f, err := os.Create(filepath.Join(mgr.dir, "cached-kernel-manifest"))
	if err != nil {
		Logger("Issue in '%s' creating the kernel cache manifest '%s':'%v'", mgr.url, mgr.dir, err)
	}
	b, err := json.Marshal(list)
	if err != nil {
		Logger("Issue in '%s' manifesting list from kernel cache manifest '%s':'%v'", mgr.url, mgr.dir, err)
	}
	io.Copy(f, bytes.NewReader(b))
	defer f.Close()

	mgr.lock.Unlock()

	return nil
}

func (mgr *RemoteManager) poll() {

	for {
		err := mgr.pollOnce(context.Background())
		if err != nil {
			time.Sleep(time.Minute)
			continue
		}

		select {
		case <-time.After(time.Hour):
		case <-mgr.ch:
			return
		}
	}

}

type remoteVersionTimestamp struct {
	Version   string    `yaml:"version"`
	Timestamp time.Time `yaml:"release"`
}

type remoteVersionsManifest struct {
	Kernels []remoteVersionTimestamp `yaml:"kernels"`
}

func (mgr *RemoteManager) remoteList(ctx context.Context) (List, error) {

	var list List

	// request remote manifest

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/manifest.txt", mgr.url), nil)
	if err != nil {
		return nil, err
	}

	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if err == context.DeadlineExceeded {
			return nil, err
		}

		getKernels := func() error {
			resp, err = http.Get(fmt.Sprintf("%s/manifest.txt", mgr.url))
			if err != nil {
				return err
			}

			return nil
		}

		for i := 0; i < 5; i++ {
			// wait 1 second and try again
			time.Sleep(time.Second * time.Duration(i+1))
			err = getKernels()
			if err == nil {
				break
			}
		}

		if err != nil {
			return nil, err
		}

	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	manifest := new(remoteVersionsManifest)
	err = yaml.Unmarshal(data, manifest)
	if err != nil {
		return nil, err
	}

	for _, kern := range manifest.Kernels {
		v, err := Parse(kern.Version)
		if err != nil {
			Logger("Remote manager for '%s' encountered an invalid entry in the remote kernels manifest: %s", mgr.url, kern.Version)
			continue
		}

		list = append(list, Tuple{
			Location: mgr.url,
			ModTime:  kern.Timestamp,
			Version:  v,
		})
	}

	return list, nil
}

func (mgr *RemoteManager) get(version CalVer) error {

	Logger("Downloading kernel version: %s", version.String())

	kernelName := filenameFromVersion(version)
	signatureName := kernelName + ".asc"

	kernelURL := fmt.Sprintf("%s/kernels/%s", mgr.url, kernelName)
	signatureURL := fmt.Sprintf("%s/kernels/%s", mgr.url, signatureName)

	kernelFile := filepath.Join(mgr.dir, kernelName)
	signatureFile := filepath.Join(mgr.dir, signatureName)
	err := removeFiles(kernelFile, signatureFile)
	if err != nil {
		return err
	}

	defer os.Remove(signatureFile)

	var success bool
	defer func() {
		if success {
			return
		}
		os.Remove(kernelFile)
		os.Remove(signatureFile)
	}()

	ch, err := grab.GetBatch(2, mgr.dir, kernelURL, signatureURL)
	if err != nil {
		return err
	}

	t := time.NewTicker(10 * time.Millisecond)
	completed := 0
	inProgress := 0
	var firstError error
	var responses []*grab.Response

	for completed < 2 {
		select {
		case resp := <-ch:
			if resp != nil {
				responses = append(responses, resp)
			}
		case <-t.C:
			for i, resp := range responses {
				if resp != nil && resp.IsComplete() {
					if resp.Err() != nil {
						err = fmt.Errorf("error downloading %s: %v", resp.Request.URL(), resp.Err())
						if firstError == nil {
							firstError = err
						}
						Logger("Remote manager for '%s' encountered an error during download: %v", mgr.url, err)
					} else if resp.HTTPResponse.StatusCode != http.StatusOK {
						err = fmt.Errorf("error downloading %s: %v -- %s", resp.Request.URL(), resp.HTTPResponse.StatusCode, http.StatusText(resp.HTTPResponse.StatusCode))
						if firstError == nil {
							firstError = err
						}
						Logger("Remote manager for '%s' encountered an error during download: %v", mgr.url, err)
					}
					responses[i] = nil
					completed++
				}
			}
			inProgress = 0
			for _, resp := range responses {
				if resp != nil {
					inProgress++
				}
			}
		}
	}

	t.Stop()

	if firstError != nil {
		return firstError
	}

	err = validateKernelSignature(kernelFile, signatureFile)
	if err != nil {
		// update to cached
		mgr.lock.Lock()
		tuple, err := mgr.cache.BestMatch(version)
		if err == nil {
			tuple.Location = strings.TrimSuffix(tuple.Location, " (cached)") + " (cached)"
			tuple.ModTime = time.Now()
			success = true
		}
		mgr.lock.Unlock()
	}

	return err
}

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

// Get ..
func (mgr *RemoteManager) Get(ctx context.Context, version CalVer) (*ManagedBundle, error) {
	var tuple *Tuple

	// Get BestMatch
	list, err := mgr.List(ctx)
	if err == nil {
		tuple, err = list.BestMatch(version)
	}

	if err != nil {
		return nil, err
	}

	if !strings.HasSuffix(tuple.Location, " (cached)") {
		var chErr error
		// cache it
		ch := make(chan error, 1)
		go func() {
			err := mgr.get(tuple.Version)
			if err != nil {
				ch <- err
			}
			close(ch)
		}()
		select {
		case chErr = <-ch:
		case <-ctx.Done():
			chErr = ctx.Err()
		}

		return nil, chErr
	}

	path := filepath.Join(mgr.dir, filenameFromVersion(tuple.Version))

	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}

	b := new(ManagedBundle)
	b.bundle, err = NewBundle(f)
	if err != nil {
		f.Close()
	} else {
		b.closer = f
		b.location = path
	}

	return b, err
}

// List ..
func (mgr *RemoteManager) List(ctx context.Context) (List, error) {

	mgr.lock.RLock()
	defer mgr.lock.RUnlock()

	var list List
	for _, elem := range mgr.cache {
		list = append(list, elem)
	}
	return list, nil
}

// Close ..
func (mgr *RemoteManager) Close() error {
	mgr.lock.Lock()
	defer mgr.lock.Unlock()
	if !mgr.closed {
		mgr.closed = true
		close(mgr.ch)
	}
	return nil
}

//
// CompoundManager
//

// CompoundManager ..
type CompoundManager struct {
	mgrs []Manager
}

// NewCompoundManager ..
func NewCompoundManager(mgrs ...Manager) (*CompoundManager, error) {
	mgr := new(CompoundManager)
	mgr.mgrs = mgrs
	return mgr, nil
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

// List ..
func (mgr *CompoundManager) List(ctx context.Context) (List, error) {

	var list List

	for Idx, sub := range mgr.mgrs {
		sl, err := sub.List(ctx)
		if err != nil {
			continue
		}
		for _, tuple := range sl {
			tuple.Idx = Idx
			list = append(list, tuple)
		}
	}

	sort.Sort(list)

	return list, nil
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
