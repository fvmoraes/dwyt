package db

import "testing"

func TestGetActiveProjectExcludesRemovedProject(t *testing.T) {
	s := newTestStore(t)
	path := "/tmp/removed-vault-project"
	p, err := s.UpsertProject(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveProject(path); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetActiveProject(p.ID); err == nil {
		t.Fatal("removed project must not be eligible for vault migration")
	}
	if historical, err := s.GetProject(p.ID); err != nil || historical.Path != path {
		t.Fatalf("historical project must remain readable: project=%#v err=%v", historical, err)
	}
}
