package ds

import (
	"testing"
)

func TestSyncMap_Set(t *testing.T) {
	sm := MakeSyncMap[int]()
	sm.Set("key1", 1)
	if sm.Get("key1") != 1 {
		t.Errorf("expected 1, got %d", sm.Get("key1"))
	}
}

func TestSyncMap_Get(t *testing.T) {
	sm := MakeSyncMap[int]()
	sm.Set("key1", 1)
	if sm.Get("key1") != 1 {
		t.Errorf("expected 1, got %d", sm.Get("key1"))
	}
	if sm.Get("key2") != 0 {
		t.Errorf("expected 0, got %d", sm.Get("key2"))
	}
}

func TestSyncMap_GetEx(t *testing.T) {
	sm := MakeSyncMap[int]()
	sm.Set("key1", 1)
	value, ok := sm.GetEx("key1")
	if !ok || value != 1 {
		t.Errorf("expected 1, got %d", value)
	}
	value, ok = sm.GetEx("key2")
	if ok || value != 0 {
		t.Errorf("expected 0, got %d", value)
	}
}

func TestSyncMap_Delete(t *testing.T) {
	sm := MakeSyncMap[int]()
	sm.Set("key1", 1)
	sm.Delete("key1")
	if sm.Get("key1") != 0 {
		t.Errorf("expected 0, got %d", sm.Get("key1"))
	}
}

func TestSyncMap_DeleteIfPointerIdentity(t *testing.T) {
	type gen struct{ id int }
	sm := MakeSyncMap[*gen]()
	g1 := &gen{id: 1}
	g2 := &gen{id: 2}
	sm.Set("j", g1)
	// Stale remover with wrong pointer must not delete.
	if sm.DeleteIf("j", func(cur *gen, exists bool) bool { return exists && cur == g2 }) {
		t.Fatal("DeleteIf with wrong pointer must be false")
	}
	if v, ok := sm.GetEx("j"); !ok || v != g1 {
		t.Fatal("expected g1 still present")
	}
	// Replacement install then stale delete of g1 must leave g2.
	sm.Set("j", g2)
	if sm.DeleteIf("j", func(cur *gen, exists bool) bool { return exists && cur == g1 }) {
		t.Fatal("stale DeleteIf must not remove replacement")
	}
	if v, ok := sm.GetEx("j"); !ok || v != g2 {
		t.Fatal("expected g2 retained after stale delete")
	}
	if !sm.DeleteIf("j", func(cur *gen, exists bool) bool { return exists && cur == g2 }) {
		t.Fatal("correct pointer DeleteIf should succeed")
	}
	if _, ok := sm.GetEx("j"); ok {
		t.Fatal("expected deleted")
	}
}

func TestSyncMap_SetUnless(t *testing.T) {
	sm := MakeSyncMap[int]()
	if !sm.SetUnless("k", 1) {
		t.Fatal("first SetUnless should succeed")
	}
	if sm.SetUnless("k", 2) {
		t.Fatal("second SetUnless must fail")
	}
	if sm.Get("k") != 1 {
		t.Fatalf("got %d", sm.Get("k"))
	}
}
