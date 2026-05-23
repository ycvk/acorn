package runtime

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRunRegistryRegisterRoot(t *testing.T) {
	reg := NewRegistry()
	budget := NewRunBudget(10)
	rc := &RunContext{
		RunID:  "run_root",
		Budget: budget,
	}

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
	budget := NewRunBudget(10)

	parent := &RunContext{
		RunID:  "run_parent",
		Budget: budget,
	}
	if err := reg.Register(parent); err != nil {
		t.Fatalf("Register parent: %v", err)
	}

	child := &RunContext{
		RunID:    "run_child",
		ParentID: "run_parent",
		Budget:   budget,
	}
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
	budget := NewRunBudget(10)

	child := &RunContext{
		RunID:    "run_orphan",
		ParentID: "run_nonexistent",
		Budget:   budget,
	}
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
	budget := NewRunBudget(10)

	root := &RunContext{RunID: "run_clear_root", Budget: budget}
	reg.Register(root)

	child1 := &RunContext{RunID: "run_clear_c1", ParentID: "run_clear_root", Budget: budget}
	reg.Register(child1)

	child2 := &RunContext{RunID: "run_clear_c2", ParentID: "run_clear_root", Budget: budget}
	reg.Register(child2)

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
	budget := NewRunBudget(10)

	root := &RunContext{RunID: "run_st_root", Budget: budget}
	reg.Register(root)

	child := &RunContext{RunID: "run_st_child", ParentID: "run_st_root", Budget: budget}
	reg.Register(child)

	grandchild := &RunContext{RunID: "run_st_gc", ParentID: "run_st_child", Budget: budget}
	reg.Register(grandchild)

	// Clear only the child subtree — root should remain
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

func TestRunRegistryInterruptTree(t *testing.T) {
	reg := NewRegistry()
	budget := NewRunBudget(10)

	root := &RunContext{RunID: "run_it_root", Budget: budget}
	reg.Register(root)

	child1 := &RunContext{RunID: "run_it_c1", ParentID: "run_it_root", Budget: budget}
	reg.Register(child1)

	child2 := &RunContext{RunID: "run_it_c2", ParentID: "run_it_root", Budget: budget}
	reg.Register(child2)

	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	ctxRoot, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	cancelFuncs := map[string]context.CancelFunc{
		"run_it_root": cancelRoot,
		"run_it_c1":   cancel1,
		"run_it_c2":   cancel2,
	}

	reg.InterruptTree("run_it_root", cancelFuncs)

	if ctx1.Err() == nil {
		t.Fatal("child1 context should be cancelled after InterruptTree")
	}
	if ctx2.Err() == nil {
		t.Fatal("child2 context should be cancelled after InterruptTree")
	}
	if ctxRoot.Err() == nil {
		t.Fatal("root context should be cancelled after InterruptTree")
	}
}

func TestRunRegistryInterruptTreeSkipsFinalizing(t *testing.T) {
	reg := NewRegistry()
	budget := NewRunBudget(10)

	root := &RunContext{RunID: "run_fin_root", Budget: budget}
	reg.Register(root)

	child := &RunContext{RunID: "run_fin_child", ParentID: "run_fin_root", Budget: budget}
	reg.Register(child)

	// Mark child as finalizing
	child.SetFinalizing()

	ctxChild, cancelChild := context.WithCancel(context.Background())
	defer cancelChild()
	ctxRoot, cancelRoot := context.WithCancel(context.Background())
	defer cancelRoot()

	cancelFuncs := map[string]context.CancelFunc{
		"run_fin_root":  cancelRoot,
		"run_fin_child": cancelChild,
	}

	reg.InterruptTree("run_fin_root", cancelFuncs)

	if ctxChild.Err() != nil {
		t.Fatal("finalizing child should NOT be cancelled")
	}
	if ctxRoot.Err() == nil {
		t.Fatal("root should still be cancelled even though child was skipped")
	}
}

func TestRunRegistryInterruptTreeLeafToRoot(t *testing.T) {
	reg := NewRegistry()
	budget := NewRunBudget(10)

	root := &RunContext{RunID: "run_ltr_root", Budget: budget}
	reg.Register(root)

	child := &RunContext{RunID: "run_ltr_child", ParentID: "run_ltr_root", Budget: budget}
	reg.Register(child)

	var order []string
	var orderMu sync.Mutex

	cancelChild := func() {
		orderMu.Lock()
		order = append(order, "child")
		orderMu.Unlock()
	}
	cancelRoot := func() {
		orderMu.Lock()
		order = append(order, "root")
		orderMu.Unlock()
	}

	cancelFuncs := map[string]context.CancelFunc{
		"run_ltr_root":  cancelRoot,
		"run_ltr_child": cancelChild,
	}

	reg.InterruptTree("run_ltr_root", cancelFuncs)

	orderMu.Lock()
	defer orderMu.Unlock()
	if len(order) != 2 {
		t.Fatalf("expected 2 cancellations, got %d", len(order))
	}
	if order[0] != "child" {
		t.Fatalf("first cancelled = %q, want %q (leaf-to-root)", order[0], "child")
	}
	if order[1] != "root" {
		t.Fatalf("second cancelled = %q, want %q (leaf-to-root)", order[1], "root")
	}
}

func TestRunContextFinalizing(t *testing.T) {
	rc := &RunContext{RunID: "run_fin_test"}

	if rc.IsFinalizing() {
		t.Fatal("new RunContext should not be finalizing")
	}

	rc.SetFinalizing()

	if !rc.IsFinalizing() {
		t.Fatal("RunContext should be finalizing after SetFinalizing")
	}
}

func TestRunContextFinalizingNil(t *testing.T) {
	var rc *RunContext
	if rc.IsFinalizing() {
		t.Fatal("nil RunContext should not report finalizing")
	}
	rc.SetFinalizing() // should not panic
}

func TestRunBudgetDefaults(t *testing.T) {
	b := NewRunBudget(10)
	if b.MaxIterations != 10 {
		t.Fatalf("MaxIterations = %d, want 10", b.MaxIterations)
	}
}

func TestRunRegistryConcurrentAccess(t *testing.T) {
	reg := NewRegistry()
	budget := NewRunBudget(10)

	root := &RunContext{RunID: "run_conc_root", Budget: budget}
	reg.Register(root)

	var wg sync.WaitGroup
	const goroutines = 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			childID := "run_conc_child_" + time.Now().Format("20060102150405.000000000")
			_ = childID
			child := &RunContext{
				RunID:    childID,
				ParentID: "run_conc_root",
				Budget:   budget,
			}
			_ = reg.Register(child)
			reg.Get(childID)
		}(i)
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
	err := reg.Register(nil)
	if err == nil {
		t.Fatal("Register nil should return error")
	}
}

func TestRunRegistryGetNonExistent(t *testing.T) {
	reg := NewRegistry()
	_, ok := reg.Get("nonexistent")
	if ok {
		t.Fatal("Get non-existent should return false")
	}
}

func TestRunRegistryClearEmpty(t *testing.T) {
	reg := NewRegistry()
	reg.Clear("nonexistent") // should not panic
}

func TestRunRegistryInterruptTreeNonExistent(t *testing.T) {
	reg := NewRegistry()
	cancelFuncs := map[string]context.CancelFunc{}
	reg.InterruptTree("nonexistent", cancelFuncs) // should not panic
}
