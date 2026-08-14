package engine

import (
	"context"
	"fmt"

	"testing"
	"time"

	"github.com/warpcode/cloakenv/internal/config"
)

func BenchmarkResolveArrayAttrCurrent(b *testing.B) {
	ctx := context.Background()
	cfg := &config.Config{
		Vaults: make(map[string]config.VaultConfig),
	}

	o, err := NewOrchestrator(cfg)
	if err != nil {
		b.Fatalf("failed to create orchestrator: %v", err)
	}

	sp := &slowProvider{delay: 1 * time.Microsecond}
	o.builtins["slow"] = sp
	o.initializedBuiltins["slow"] = true

	var arr []string
	for i := 0; i < 1000; i++ {
		arr = append(arr, fmt.Sprintf("slow://loc_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := o.resolveAttrRecursive(ctx, arr, 0)
		if err != nil {
			b.Fatal(err)
		}
	}
}
