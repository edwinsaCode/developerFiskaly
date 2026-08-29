package project

import (
	"context"
	"fmt"
	"log"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// SeedLITHOS inserts the LITHOS Villas project with its 4 phases and 18 units
// for the given tenant. Fully idempotent — safe to call multiple times.
// Uses FirstOrCreate keyed on (tenant_id, name) for project/phases and
// (project_id, code) for units; skips any record that already exists.
//
// Layout:
//
//	Fase 1 (6 unit)    — LITHOS-A01..A06  Villa Type A,        150 sqm, Rp 2.8 M
//	Fase 2 (6 unit)    — LITHOS-B01..B06  Villa Type B,        200 sqm, Rp 3.5 M
//	Fase 3 (4 unit)    — LITHOS-C01..C04  Villa Type C,        250 sqm, Rp 4.2 M
//	Flagship (2 unit)  — LITHOS-F01..F02  Villa Flagship,      350 sqm, Rp 7.0 M
func SeedLITHOS(ctx context.Context, db *gorm.DB, tenantID uint64) error {
	log.Printf("[SEED] LITHOS Villas — tenant %d", tenantID)

	// ── Project ───────────────────────────────────────────────────────────────

	p := Project{
		TenantID: tenantID,
		Name:     "LITHOS Villas",
		Status:   ProjectStatusPlanning,
		LandArea: decimal.NewFromFloat(15000), // 1.5 ha
		Notes:    "Perumahan villa premium di kawasan Bali — 18 unit, 4 fase.",
	}
	res := db.WithContext(ctx).
		Where("tenant_id = ? AND name = ?", tenantID, p.Name).
		FirstOrCreate(&p)
	if res.Error != nil {
		return fmt.Errorf("SeedLITHOS: project: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		log.Printf("[SEED] Project skipped (id=%d)", p.ID)
	} else {
		log.Printf("[SEED] Project created (id=%d)", p.ID)
	}

	// ── Phases ────────────────────────────────────────────────────────────────

	type phaseSpec struct {
		name        string
		description string
		targetUnits int
	}
	phaseSpecs := []phaseSpec{
		{"Fase 1", "Kluster A — 6 villa tipe standar", 6},
		{"Fase 2", "Kluster B — 6 villa tipe medium", 6},
		{"Fase 3", "Kluster C — 4 villa tipe premium", 4},
		{"Flagship", "2 villa eksklusif dengan kolam renang privat", 2},
	}

	phases := make([]*ProjectPhase, len(phaseSpecs))
	pCreated, pSkipped := 0, 0
	for i, spec := range phaseSpecs {
		ph := ProjectPhase{
			TenantID:    tenantID,
			ProjectID:   p.ID,
			Name:        spec.name,
			Description: spec.description,
			TargetUnits: spec.targetUnits,
			Status:      PhaseStatusPlanning,
		}
		res := db.WithContext(ctx).
			Where("tenant_id = ? AND project_id = ? AND name = ?", tenantID, p.ID, spec.name).
			FirstOrCreate(&ph)
		if res.Error != nil {
			return fmt.Errorf("SeedLITHOS: fase %q: %w", spec.name, res.Error)
		}
		if res.RowsAffected == 0 {
			pSkipped++
		} else {
			pCreated++
		}
		phases[i] = &ph
	}
	log.Printf("[SEED] Phases: %d created, %d skipped", pCreated, pSkipped)

	// ── Units ─────────────────────────────────────────────────────────────────

	type unitSpec struct {
		phaseIndex int
		code       string
		unitType   string
		areaM2     float64
		listPrice  int64
	}
	unitSpecs := []unitSpec{
		// Fase 1 — LITHOS-A01..A06
		{0, "LITHOS-A01", "villa", 150, 2_800_000_000},
		{0, "LITHOS-A02", "villa", 150, 2_800_000_000},
		{0, "LITHOS-A03", "villa", 150, 2_800_000_000},
		{0, "LITHOS-A04", "villa", 150, 2_800_000_000},
		{0, "LITHOS-A05", "villa", 150, 2_800_000_000},
		{0, "LITHOS-A06", "villa", 150, 2_800_000_000},
		// Fase 2 — LITHOS-B01..B06
		{1, "LITHOS-B01", "villa", 200, 3_500_000_000},
		{1, "LITHOS-B02", "villa", 200, 3_500_000_000},
		{1, "LITHOS-B03", "villa", 200, 3_500_000_000},
		{1, "LITHOS-B04", "villa", 200, 3_500_000_000},
		{1, "LITHOS-B05", "villa", 200, 3_500_000_000},
		{1, "LITHOS-B06", "villa", 200, 3_500_000_000},
		// Fase 3 — LITHOS-C01..C04
		{2, "LITHOS-C01", "villa", 250, 4_200_000_000},
		{2, "LITHOS-C02", "villa", 250, 4_200_000_000},
		{2, "LITHOS-C03", "villa", 250, 4_200_000_000},
		{2, "LITHOS-C04", "villa", 250, 4_200_000_000},
		// Flagship — LITHOS-F01..F02
		// TODO(tax-advisor): klasifikasi "rumah mewah" per PMK terbaru menentukan
		// apakah tarif PPN 12% PENUH berlaku (bukan mekanisme DPP 11/12).
		{3, "LITHOS-F01", "villa-flagship", 350, 7_000_000_000},
		{3, "LITHOS-F02", "villa-flagship", 350, 7_000_000_000},
	}

	uCreated, uSkipped := 0, 0
	for _, spec := range unitSpecs {
		phID := phases[spec.phaseIndex].ID
		u := Unit{
			TenantID:     tenantID,
			ProjectID:    p.ID,
			PhaseID:      &phID,
			Code:         spec.code,
			UnitType:     spec.unitType,
			SaleableArea: decimal.NewFromFloat(spec.areaM2),
			ListPrice:    domain.FromInt(spec.listPrice),
			Status:       UnitStatusAvailable,
		}
		res := db.WithContext(ctx).
			Where("project_id = ? AND code = ?", p.ID, spec.code).
			FirstOrCreate(&u)
		if res.Error != nil {
			return fmt.Errorf("SeedLITHOS: unit %q: %w", spec.code, res.Error)
		}
		if res.RowsAffected == 0 {
			uSkipped++
		} else {
			uCreated++
		}
	}
	log.Printf("[SEED] Units: %d created, %d skipped", uCreated, uSkipped)

	return nil
}
