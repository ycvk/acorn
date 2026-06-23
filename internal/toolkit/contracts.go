package toolkit

import (
	"context"
	"fmt"
	"strings"

	einotool "github.com/cloudwego/eino/components/tool"
)

type ToolLoadingMode string

const (
	ToolLoadingModeEager    ToolLoadingMode = "eager"
	ToolLoadingModeDeferred ToolLoadingMode = "deferred"
	ToolLoadingModeHidden   ToolLoadingMode = "hidden"
)

type ToolLoadingPolicy struct {
	Mode   ToolLoadingMode
	Reason string
}

type ToolExecutionPolicy struct {
	ParallelPolicy ParallelPolicy
	PathArg        string
}

type ToolContract struct {
	Name      string
	Source    string
	Kind      ToolKind
	Category  ToolCategory
	Loading   ToolLoadingPolicy
	Execution ToolExecutionPolicy
}

func (c ToolContract) normalized() ToolContract {
	c.Name = strings.TrimSpace(c.Name)
	c.Source = strings.TrimSpace(c.Source)
	c.Loading.Reason = strings.TrimSpace(c.Loading.Reason)
	c.Execution.PathArg = strings.TrimSpace(c.Execution.PathArg)
	return c
}

func (c ToolContract) Validate() error {
	c = c.normalized()
	if c.Name == "" {
		return fmt.Errorf("tool contract has empty name")
	}
	if c.Source == "" {
		return fmt.Errorf("tool contract %q has empty source", c.Name)
	}
	if c.Kind == "" {
		return fmt.Errorf("tool contract %q has empty kind", c.Name)
	}
	if c.Category == "" {
		return fmt.Errorf("tool contract %q has empty category", c.Name)
	}
	switch c.Loading.Mode {
	case ToolLoadingModeEager, ToolLoadingModeHidden:
	case ToolLoadingModeDeferred:
		if c.Loading.Reason == "" {
			return fmt.Errorf("tool contract %q deferred loading requires reason", c.Name)
		}
	default:
		return fmt.Errorf("tool contract %q has unknown loading mode %q", c.Name, c.Loading.Mode)
	}
	switch c.Execution.ParallelPolicy {
	case ParallelPolicyReadOnly, ParallelPolicySerial:
	default:
		return fmt.Errorf("tool contract %q has unknown parallel policy %q", c.Name, c.Execution.ParallelPolicy)
	}
	return nil
}

func EagerLoadingPolicy() ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeEager}
}

func DeferredLoadingPolicy(reason string) ToolLoadingPolicy {
	return ToolLoadingPolicy{Mode: ToolLoadingModeDeferred, Reason: strings.TrimSpace(reason)}
}

type ToolProgressEvent struct {
	Delta string
}

type ToolProgressEmitter func(ctx context.Context, event ToolProgressEvent) error

type ProgressTool interface {
	einotool.BaseTool
	InvokableRunWithProgress(ctx context.Context, argumentsInJSON string, emit ToolProgressEmitter, opts ...einotool.Option) (string, error)
}
