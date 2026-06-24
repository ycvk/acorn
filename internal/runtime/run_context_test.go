package runtime

import (
	"sync"
	"testing"
	"time"
)

func TestRunRegistryRegisterRoot(t *testing.T) {
	reg := NewRegistry()
	rc := &RunContext{RunID: "run_root"}

	if err := reg.Register(rc); err != nil {
		t.Fatalf("Register root: %v", err)
	}

	got, ok := reg.Get("run_root")
	if !ok {
		t.Fatal("Get root: not found")
	}
	if got.Depth != 0 {
		t.Fatalf("root depth = %d, want 0", got.Depth)
	}
	if got.RunID != "run_root" {
		t.Fatalf("root RunID = %q, want %q", got.RunID, "run_root")
	}
}

func TestRunRegistryRegisterChild(t *testing.T) {
	reg := NewRegistry()

	parent := &RunContext{RunID: "run_parent"}
	if err := reg.Register(parent); err != nil {
		t.Fatalf("Register parent: %v", err)
	}

	child := &RunContext{RunID: "run_child", ParentID: "run_parent"}
	if err := reg.Register(child); err != nil {
		t.Fatalf("Register child: %v", err)
	}

	got, ok := reg.Get("run_child")
	if !ok {
		t.Fatal("Get child: not found")
	}
	if got.Depth != 1 {
		t.Fatalf("child depth = %d, want 1", got.Depth)
	}

	parentGot, _ := reg.Get("run_parent")
	if len(parentGot.ChildIDs) != 1 || parentGot.ChildIDs[0] != "run_child" {
		t.Fatalf("parent ChildIDs = %v, want [run_child]", parentGot.ChildIDs)
	}
}

func TestRunRegistryRegisterMissingParent(t *testing.T) {
	reg := NewRegistry()

	child := &RunContext{RunID: "run_orphan", ParentID: "run_nonexistent"}
	err := reg.Register(child)
	if err == nil {
		t.Fatal("Register with missing parent: expected error, got nil")
	}
	if got, want := err.Error(), "parent run run_nonexistent not found"; got != want {
		t.Fatalf("error = %q, want %q", got, want)
	}
}

func TestRunRegistryClear(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&RunContext{RunID: "run_clear_root"})
	reg.Register(&RunContext{RunID: "run_clear_c1", ParentID: "run_clear_root"})
	reg.Register(&RunContext{RunID: "run_clear_c2", ParentID: "run_clear_root"})

	reg.Clear("run_clear_root")

	if _, ok := reg.Get("run_clear_root"); ok {
		t.Fatal("root should be removed after Clear")
	}
	if _, ok := reg.Get("run_clear_c1"); ok {
		t.Fatal("child1 should be removed after Clear")
	}
	if _, ok := reg.Get("run_clear_c2"); ok {
		t.Fatal("child2 should be removed after Clear")
	}
}

func TestRunRegistryClearSubtree(t *testing.T) {
	reg := NewRegistry()

	reg.Register(&RunContext{RunID: "run_st_root"})
	reg.Register(&RunContext{RunID: "run_st_child", ParentID: "run_st_root"})
	reg.Register(&RunContext{RunID: "run_st_gc", ParentID: "run_st_child"})

	// Clear only the child subtree — root should remain.
	reg.Clear("run_st_child")

	if _, ok := reg.Get("run_st_root"); !ok {
		t.Fatal("root should remain after clearing child subtree")
	}
	if _, ok := reg.Get("run_st_child"); ok {
		t.Fatal("child should be removed after Clear")
	}
	if _, ok := reg.Get("run_st_gc"); ok {
		t.Fatal("grandchild should be removed after Clear")
	}
}

func TestRunRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	reg.Register(&RunContext{RunID: "run_conc_root"})

	var wg sync.WaitGroup
	const goroutines = 50

	for range goroutines {
		wg.Go(func() {
			childID := "run_conc_child_" + time.Now().Format("20060102150405.000000000")
			child := &RunContext{RunID: childID, ParentID: "run_conc_root"}
			_ = reg.Register(child)
			reg.Get(childID)
		})
	}

	wg.Wait()

	rootGot, ok := reg.Get("run_conc_root")
	if !ok {
		t.Fatal("root should exist after concurrent access")
	}
	if len(rootGot.ChildIDs) == 0 {
		t.Fatal("root should have at least one child after concurrent registrations")
	}
}

func TestRunRegistryRegisterNil(t *testing.T) {
	reg := NewRegistry()
	if err := reg.Register(nil); err == nil {
		t.Fatal("Register nil should return error")
	}
}

func TestRunRegistryGetNonExistent(t *testing.T) {
	reg := NewRegistry()
	if _, ok := reg.Get("nonexistent"); ok {
		t.Fatal("Get non-existent should return false")
	}
}

func TestRunRegistryClearEmpty(t *testing.T) {
	reg := NewRegistry()
	reg.Clear("nonexistent") // should not panic
}
