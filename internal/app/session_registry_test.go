package app

import "testing"

func TestRegistryAddGetDeleteAll(t *testing.T) {
	registry := NewRegistry()
	session := &Session{ID: "s1", Cwd: "/tmp"}

	registry.Add(session)
	got, ok := registry.Get("s1")
	if !ok || got != session {
		t.Fatalf("get = %+v, %v", got, ok)
	}
	if all := registry.All(); len(all) != 1 || all[0] != session {
		t.Fatalf("all = %+v", all)
	}
	deleted, ok := registry.Delete("s1")
	if !ok || deleted != session {
		t.Fatalf("delete = %+v, %v", deleted, ok)
	}
	if _, ok := registry.Get("s1"); ok {
		t.Fatal("session still exists")
	}
}
