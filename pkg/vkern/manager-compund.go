package vkern

import (
	"context"
	"sort"
)

// CompoundManager ..
type CompoundManager struct {
	mgrs []Manager
}

// Latest ..
func (mgr *CompoundManager) Latest() (string, error) {
	return latest(mgr)
}

// NewCompoundManager ..
func NewCompoundManager(mgrs ...Manager) (*CompoundManager, error) {
	mgr := new(CompoundManager)
	mgr.mgrs = mgrs
	return mgr, nil
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
