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

	"github.com/vorteil/vorteil/pkg/elog"
	"golang.org/x/crypto/openpgp"
	"gopkg.in/yaml.v2"
)

var CLIUpdateInterval time.Duration = time.Hour * 24

type CLIRemoteManager struct {
	dir        string
	url        string
	cache      List
	nextUpdate time.Time
	log        elog.View
}

func (mgr *CLIRemoteManager) updateList(ctx context.Context) (List, error) {
	var list List
	list = mgr.cache

	// request remote manifest

	req, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/manifest.txt", mgr.url), nil)
	if err != nil {
		return list, err
	}

	req = req.WithContext(ctx)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		mgr.log.Errorf("error in request to remote kernels source: %w", err)
		return list, nil
	}
	defer resp.Body.Close()

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		mgr.log.Errorf("error reading response from remote kernels source: %w", err)
		return list, nil
	}

	manifest := new(remoteVersionsManifest)
	err = yaml.Unmarshal(data, manifest)
	if err != nil {
		mgr.log.Errorf("unable to parse response from remote kernels source: %w", err)
		return list, nil
	}

	for _, kern := range manifest.Kernels {
		v, err := Parse(kern.Version)
		if err != nil {
			mgr.log.Errorf("encountered an invalid entry '%s' in the remote kernels manifest: %s", kern.Version, mgr.url)
			continue
		}

		list = append(list, Tuple{
			Location: mgr.url,
			ModTime:  kern.Timestamp,
			Version:  v,
		})
	}

	for i, tuple := range list {
		fi, err := os.Stat(filepath.Join(mgr.dir, filenameFromVersion(tuple.Version)))
		if err == nil {
			if tuple.ModTime.Before(fi.ModTime()) {
				list[i].Location += " (cached)"
			} else {
				mgr.log.Warnf("detected an unusual remote kernel file update for source '%s' on kernel '%s'", mgr.url, tuple.Version)
				err = os.Remove(filepath.Join(mgr.dir, filenameFromVersion(tuple.Version)))
				if err != nil {
					mgr.log.Errorf("error removing file in remote kernels cache directory '%s': %v", mgr.dir, err)
				}
			}
		} else if os.IsNotExist(err) {
			continue
		} else {
			mgr.log.Errorf("error in '%s' remote kernels cache directory '%s': %v", mgr.url, mgr.dir, err)
		}
	}

	return list, nil
}

func (mgr *CLIRemoteManager) flushCache() error {
	f, err := os.Create(filepath.Join(mgr.dir, "cached-kernel-manifest"))
	if err != nil {
		mgr.log.Errorf("error creating the kernel cache manifest '%s':'%v'", mgr.dir, err)
	}
	b, err := json.Marshal(mgr.cache)
	if err != nil {
		panic(err)
	}
	_, err = io.Copy(f, bytes.NewReader(b))
	if err != nil {
		mgr.log.Errorf("error writing updated manifest list from kernel cache manifest '%s':'%v'", mgr.dir, err)
	}
	defer f.Close()

	err = f.Close()
	if err != nil {
		mgr.log.Errorf("error saving manifest list from kernel cache manifest '%s':'%v'", mgr.dir, err)
	}

	mgr.nextUpdate = time.Now().Add(CLIUpdateInterval)
	return nil
}

func (mgr *CLIRemoteManager) update(ctx context.Context) error {
	if !time.Now().After(mgr.nextUpdate) {
		return nil
	}

	err := ctx.Err()
	if err != nil {
		return err
	}

	progress := mgr.log.NewProgress(fmt.Sprintf("Checking %s for updates", mgr.url), "", 0)
	defer progress.Finish(false)

	_, err = ioutil.ReadFile(filepath.Join(mgr.dir, "cached-kernel-manifest"))
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
	}

	mgr.cache, err = mgr.updateList(ctx)
	if err != nil {
		return err
	}

	err = mgr.flushCache()
	if err != nil {
		return err
	}

	progress.Finish(true)
	return nil
}

func (mgr *CLIRemoteManager) get(version CalVer) error {

	prog := mgr.log.NewProgress(fmt.Sprintf("Fetching kernel: %s", version.String()), "", 0)
	defer prog.Finish(false)

	mgr.log.Infof("Downloading kernel version: %s", version.String())

	kernelName := filenameFromVersion(version)
	signatureName := kernelName + ".asc"

	kernelURL := fmt.Sprintf("%s/kernels/%s", mgr.url, kernelName)
	signatureURL := fmt.Sprintf("%s/kernels/%s", mgr.url, signatureName)

	kernelFile := filepath.Join(mgr.dir, kernelName)
	err := os.Remove(kernelFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	signatureFile := filepath.Join(mgr.dir, signatureName)
	err = os.Remove(signatureFile)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	defer os.Remove(signatureFile)

	var success bool
	defer func() {
		if success {
			return
		}
		_ = os.Remove(kernelFile)
		_ = os.Remove(signatureFile)
	}()

	var wg sync.WaitGroup
	wg.Add(2)

	var firstError error
	var firstErrorLock sync.Mutex

	setFirstError := func(err error) {
		firstErrorLock.Lock()
		defer firstErrorLock.Unlock()
		if firstError == nil {
			firstError = err
		}
	}

	download := func(src, dest string) {
		defer wg.Done()

		f, err := os.Create(dest)
		if err != nil {
			setFirstError(fmt.Errorf("error creating kernel file '%s': %w", dest, err))
			return
		}
		defer f.Close()

		resp, err := http.Get(src)
		if err != nil {
			setFirstError(fmt.Errorf("error in request for file at url '%s': %w", src, err))
			return
		}

		if resp.Body != nil {
			defer resp.Body.Close()
		}

		p := mgr.log.NewProgress(fmt.Sprintf("Downloading file from url: %s", src), "KiB", resp.ContentLength)
		defer p.Finish(false)

		r := p.ProxyReader(resp.Body)
		defer r.Close()

		_, err = io.Copy(f, r)
		if err != nil {
			setFirstError(fmt.Errorf("error downloading kernel file '%s': %w", dest, err))
			return
		}

		err = f.Close()
		if err != nil {
			setFirstError(fmt.Errorf("error saving kernel file '%s': %w", dest, err))
			return
		}

		p.Finish(true)
		return
	}

	go download(kernelURL, kernelFile)
	go download(signatureURL, signatureFile)

	wg.Wait()

	if firstError != nil {
		return firstError
	}

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
	if err != nil {
		return err
	}

	err = ker.Close()
	if err != nil {
		return err
	}

	err = sig.Close()
	if err != nil {
		return err
	}

	// update to cached

	tuple, err := mgr.cache.BestMatch(version)
	if err == nil {
		tuple.Location = strings.TrimSuffix(tuple.Location, " (cached)") + " (cached)"
		tuple.ModTime = time.Now()
		success = true
	}

	err = mgr.flushCache()
	if err != nil {
		return err
	}

	prog.Finish(true)

	return nil
}

func (mgr *CLIRemoteManager) Get(ctx context.Context, version CalVer) (*ManagedBundle, error) {

	list, err := mgr.List(ctx)
	if err != nil {
		return nil, err
	}

	tuple, err := list.BestMatch(version)
	if err != nil {
		return nil, err
	}

	if !strings.HasSuffix(tuple.Location, " (cached)") {
		// cache it
		err = func() error {
			err := mgr.get(tuple.Version)
			if err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
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
		return nil, err
	}
	b.closer = f
	b.location = path
	return b, nil

}

func (mgr *CLIRemoteManager) List(ctx context.Context) (List, error) {

	err := mgr.update(ctx)
	if err != nil {
		return nil, err
	}

	var list List
	for _, elem := range mgr.cache {
		list = append(list, elem)
	}
	return list, nil

}

func (mgr *CLIRemoteManager) Latest() (out string, err error) {
	return latest(mgr)
}

func NewCLIRemoteManager(url, dir string, logger elog.View) (*CLIRemoteManager, error) {
	mgr := new(CLIRemoteManager)
	mgr.log = logger
	mgr.url = url
	mgr.dir = dir
	err := os.MkdirAll(dir, 0777)
	if err != nil {
		return nil, err
	}
	mgr.nextUpdate = time.Now()
	var list List
	mgr.cache = list

	if fi, err := os.Stat(filepath.Join(mgr.dir, "cached-kernel-manifest")); err == nil {
		f, err := os.Open(filepath.Join(mgr.dir, "cached-kernel-manifest"))
		if err != nil {
			mgr.log.Errorf("Unable to open the cached kernels file: '%s'", err)
			return mgr, nil
		}
		b, err := ioutil.ReadAll(f)
		if err != nil {
			mgr.log.Errorf("Unable to read the cached kernels file: '%s'", err)
			return mgr, nil
		}
		err = json.Unmarshal(b, &list)
		if err != nil {
			mgr.log.Errorf("Unable to process cached kernels file: '%s'", err)
			return mgr, nil
		}
		mgr.cache = list
		mgr.nextUpdate = fi.ModTime().Add(CLIUpdateInterval)
	}

	return mgr, nil
}

type CLIArgs struct {
	Directory          string   `toml:"directory"`
	DropPath           string   `toml:"drop-path"`
	RemoteRepositories []string `toml:"remote-repositories"`
}

func CLI(args CLIArgs, logger elog.View) (Manager, error) {

	var mgrs []Manager

	if args.DropPath != "" {
		m, err := NewLocalManager(args.DropPath)
		if err != nil {
			return nil, err
		}
		mgrs = append(mgrs, m)
	}

	for _, s := range args.RemoteRepositories {
		m, err := NewCLIRemoteManager(s, filepath.Join(args.Directory, strings.ReplaceAll(strings.TrimPrefix(strings.TrimPrefix(s, "https://"), "http://"), "/", "_")), logger)
		if err != nil {
			return nil, err
		}
		mgrs = append(mgrs, m)
	}

	return NewCompoundManager(mgrs...)
}
