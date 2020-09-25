package vkern

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cavaliercoder/grab"
	"gopkg.in/yaml.v2"
)

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
		if !success {
			os.Remove(kernelFile)
			os.Remove(signatureFile)
		}
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
