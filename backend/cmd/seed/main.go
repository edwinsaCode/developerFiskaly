package main

import (
	"context"
	"flag"
	"log"

	"esaproperti/internal/ledger"
	"esaproperti/internal/platform/config"
	"esaproperti/internal/platform/db"
	"esaproperti/internal/project"
)

func main() {
	tenantID := flag.Uint64("tenant", 0, "tenant ID to seed COA for (required)")
	flag.Parse()
	if *tenantID == 0 {
		log.Fatal("--tenant <id> is required")
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	gdb, err := db.Connect(cfg.DBDSN, cfg.IsDevelopment())
	if err != nil {
		log.Fatalf("database: %v", err)
	}

	if err := ledger.SeedCOA(context.Background(), gdb, *tenantID); err != nil {
		log.Fatalf("seed COA: %v", err)
	}
	log.Printf("COA seeded successfully for tenant %d", *tenantID)

	if err := project.SeedLITHOS(context.Background(), gdb, *tenantID); err != nil {
		log.Fatalf("seed LITHOS: %v", err)
	}
	log.Printf("LITHOS Villas seeded successfully for tenant %d", *tenantID)
}
