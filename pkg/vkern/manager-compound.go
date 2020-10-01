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

// Get ..
func (mgr *CompoundManager) Get(ctx context.Context, version CalVer) (*ManagedBundle, error) {
	var err error
	var b *ManagedBundle

	list, err := mgr.List(ctx)
	if err != nil {
		return nil, err
	}

	tuple, err := list.BestMatch(version)
	if err == nil {
		return mgr.mgrs[tuple.Idx].Get(ctx, tuple.Version)
	}

	// this branch in logic exists to handle CLI-style situations where the list might not update until the get call
	for _, m := range mgr.mgrs {
		b, err = m.Get(ctx, version)
		if err == nil {
			break
		}
	}

	return b, err
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
