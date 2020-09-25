package vkern

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"time"
)

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
