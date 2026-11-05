package main

import (
	"context"
	"fmt"

	enginecontract "github.com/yyhuni/lunafox/engines/dork_discovery/contract"
	dorkruntime "github.com/yyhuni/lunafox/engines/dork_discovery/runtime"
)

func main() {
	Run(runEngine)
}

func runEngine(ctx context.Context, execution *enginecontract.Execution) error {
	if ctx == nil {
		return fmt.Errorf("execution context is required")
	}
	if execution == nil {
		return fmt.Errorf("execution is required")
	}
	return dorkruntime.New().Execute(ctx, execution)
}
