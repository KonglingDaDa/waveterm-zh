// Copyright 2025, Command Line Inc.
// SPDX-License-Identifier: Apache-2.0

package ds

import "sync"

type SyncMap[T any] struct {
	lock *sync.Mutex
	m    map[string]T
}

func MakeSyncMap[T any]() *SyncMap[T] {
	return &SyncMap[T]{
		lock: &sync.Mutex{},
		m:    make(map[string]T),
	}
}

func (sm *SyncMap[T]) Set(key string, value T) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	sm.m[key] = value
}

func (sm *SyncMap[T]) Get(key string) T {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	return sm.m[key]
}

func (sm *SyncMap[T]) GetEx(key string) (T, bool) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	v, ok := sm.m[key]
	return v, ok
}

func (sm *SyncMap[T]) Delete(key string) {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	delete(sm.m, key)
}

// DeleteIf deletes key only when pred(current, exists) is true, atomically
// under the map lock. Use pointer identity in pred for compare-and-delete of
// generation owners so a stale remover cannot drop a newer replacement.
func (sm *SyncMap[T]) DeleteIf(key string, pred func(current T, exists bool) bool) bool {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	v, ok := sm.m[key]
	if !pred(v, ok) {
		return false
	}
	delete(sm.m, key)
	return true
}

func (sm *SyncMap[T]) SetUnless(key string, value T) bool {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	if _, exists := sm.m[key]; exists {
		return false
	}
	sm.m[key] = value
	return true
}

func (sm *SyncMap[T]) TestAndSet(key string, newValue T, testFn func(T, bool) bool) bool {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	currentValue, exists := sm.m[key]
	if testFn(currentValue, exists) {
		sm.m[key] = newValue
		return true
	}
	return false
}

func (sm *SyncMap[T]) GetOrCreate(key string, createFn func() T) T {
	sm.lock.Lock()
	defer sm.lock.Unlock()
	if v, ok := sm.m[key]; ok {
		return v
	}
	v := createFn()
	sm.m[key] = v
	return v
}
