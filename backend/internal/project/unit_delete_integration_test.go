//go:build integration

package project_test

// Hapus unit: hanya unit tanpa relasi apa pun yang boleh dihapus; unit yang
// sudah dipakai ditolak dengan alasan, tanpa menyentuh histori maupun unit lain.

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

const udTenant uint64 = 9_900_701
const udOtherTenant uint64 = 9_900_702

func TestIntegration_DeleteUnit(t *testing.T) {
	db := ubConnect(t)
	ctx := context.Background()
	cleanup := func() {
		for _, tbl := range []string{"journal_lines", "journal_entries", "approval_requests",
			"unit_status_transitions", "units", "product_types", "project_phases", "projects", "accounts"} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id IN (?,?)", udTenant, udOtherTenant).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
	}
	cleanup()
	t.Cleanup(cleanup)

	repo := project.NewGORMRepository(db)
	svc := project.NewService(repo, repo, repo)
	svc.SetProductTypeStore(repo)
	if err := ledger.SeedCOA(ctx, db, udTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		udTenant, "Hapus Unit", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	var pid uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&pid)
	if _, err := svc.CreateProductType(ctx, udTenant, &project.ProductType{
		Code: "rumah", Name: "Rumah", Category: project.ProductCategoryProperty,
	}); err != nil {
		t.Fatalf("seed product type: %v", err)
	}

	units, err := svc.BulkCreateUnits(ctx, udTenant, project.BulkCreateUnitsRequest{
		ProjectID: pid, Block: "Z", UnitStart: 1, UnitEnd: 5, UnitType: "rumah",
		SaleableArea: decimal.NewFromInt(36), ListPrice: domain.FromInt(300_000_000),
	})
	if err != nil {
		t.Fatalf("bulk create: %v", err)
	}
	unused, transitioned, journaled, sibling, legacy := units[0], units[1], units[2], units[3], units[4]

	// Unit lama: hanya punya baris backfill sintetis (migrasi 000043) — itu
	// titik awal audit, bukan pemakaian; unit tetap boleh dihapus.
	if err := db.Exec(`INSERT INTO unit_status_transitions
		(tenant_id, unit_id, from_status, to_status, event, event_date, reference_type, created_at, updated_at)
		VALUES (?, ?, '', 'available', 'backfill', NOW(3), 'backfill', NOW(3), NOW(3))`, udTenant, legacy.ID).Error; err != nil {
		t.Fatalf("seed backfill: %v", err)
	}

	// Unit dipakai #1: transisi status (FK nyata ke units).
	if _, err := svc.Transition(ctx, udTenant, transitioned.ID, project.TransitionRequest{Next: project.UnitStatusReserved}); err != nil {
		t.Fatalf("transition: %v", err)
	}
	// Unit dipakai #2: baris jurnal (FK logis — DB sendiri tidak akan menolak).
	if err := db.Exec(`INSERT INTO journal_entries (tenant_id, date) VALUES (?, CURDATE())`, udTenant).Error; err != nil {
		t.Fatalf("seed journal entry: %v", err)
	}
	var jeID, accID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&jeID)
	db.Raw("SELECT id FROM accounts WHERE tenant_id = ? ORDER BY id LIMIT 1", udTenant).Scan(&accID)
	if err := db.Exec(`INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, unit_id) VALUES (?,?,?,?)`,
		udTenant, jeID, accID, journaled.ID).Error; err != nil {
		t.Fatalf("seed journal line: %v", err)
	}

	t.Run("tenant lain tidak bisa menghapus", func(t *testing.T) {
		if err := svc.DeleteUnit(ctx, udOtherTenant, unused.ID); !errors.Is(err, project.ErrUnitNotFound) {
			t.Fatalf("want ErrUnitNotFound, got %v", err)
		}
		if _, err := svc.GetUnit(ctx, udTenant, unused.ID); err != nil {
			t.Fatalf("unit harus tetap ada: %v", err)
		}
	})

	t.Run("unit yang sudah ditransisi ditolak", func(t *testing.T) {
		err := svc.DeleteUnit(ctx, udTenant, transitioned.ID)
		var inUse *project.UnitInUseError
		if !errors.As(err, &inUse) || !errors.Is(err, project.ErrUnitInUse) {
			t.Fatalf("want UnitInUseError, got %v", err)
		}
		if len(inUse.Reasons) < 2 { // status reserved + riwayat transisi
			t.Fatalf("alasan kurang lengkap: %v", inUse.Reasons)
		}
		var n int64
		db.Raw("SELECT COUNT(*) FROM unit_status_transitions WHERE tenant_id = ? AND unit_id = ?", udTenant, transitioned.ID).Scan(&n)
		if n != 1 {
			t.Fatalf("riwayat transisi berubah: %d", n)
		}
	})

	t.Run("unit yang punya jurnal ditolak, jurnal utuh", func(t *testing.T) {
		err := svc.DeleteUnit(ctx, udTenant, journaled.ID)
		var inUse *project.UnitInUseError
		if !errors.As(err, &inUse) {
			t.Fatalf("want UnitInUseError, got %v", err)
		}
		if len(inUse.Reasons) != 1 || inUse.Reasons[0] != "sudah tercatat di jurnal akuntansi" {
			t.Fatalf("alasan: %v", inUse.Reasons)
		}
		var n int64
		db.Raw("SELECT COUNT(*) FROM journal_lines WHERE tenant_id = ? AND unit_id = ?", udTenant, journaled.ID).Scan(&n)
		if n != 1 {
			t.Fatalf("baris jurnal berubah: %d", n)
		}
	})

	t.Run("unit lama dengan backfill saja bisa dihapus", func(t *testing.T) {
		if err := svc.DeleteUnit(ctx, udTenant, legacy.ID); err != nil {
			t.Fatalf("delete legacy: %v", err)
		}
		var n int64
		db.Raw("SELECT COUNT(*) FROM unit_status_transitions WHERE tenant_id = ? AND unit_id = ?", udTenant, legacy.ID).Scan(&n)
		if n != 0 {
			t.Fatalf("backfill unit terhapus masih tersisa: %d", n)
		}
	})

	t.Run("unit unused terhapus, unit lain utuh", func(t *testing.T) {
		if err := svc.DeleteUnit(ctx, udTenant, unused.ID); err != nil {
			t.Fatalf("delete: %v", err)
		}
		if _, err := svc.GetUnit(ctx, udTenant, unused.ID); !errors.Is(err, project.ErrUnitNotFound) {
			t.Fatalf("unit masih ada: %v", err)
		}
		list, err := svc.ListUnitsByProject(ctx, udTenant, pid)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		got := map[uint64]project.UnitStatus{}
		for _, u := range list {
			got[u.ID] = u.Status
		}
		if _, ok := got[unused.ID]; ok {
			t.Fatal("unit terhapus masih muncul di daftar")
		}
		want := map[uint64]project.UnitStatus{
			transitioned.ID: project.UnitStatusReserved,
			journaled.ID:    project.UnitStatusAvailable,
			sibling.ID:      project.UnitStatusAvailable,
		}
		for id, st := range want {
			if got[id] != st {
				t.Fatalf("unit %d: status %q, want %q", id, got[id], st)
			}
		}
		if err := svc.DeleteUnit(ctx, udTenant, unused.ID); !errors.Is(err, project.ErrUnitNotFound) {
			t.Fatalf("hapus ulang: want ErrUnitNotFound, got %v", err)
		}
	})
}

// Setiap kolom `unit_id` di schema WAJIB diperiksa guard hapus unit. Tabel baru
// yang menunjuk unit tanpa didaftarkan di unitUsageChecks → test ini merah,
// sebelum unit berhistori bisa terhapus diam-diam.
func TestIntegration_DeleteUnit_GuardCoversEveryUnitIDColumn(t *testing.T) {
	db := ubConnect(t)
	var tables []string
	if err := db.Raw(`SELECT table_name FROM information_schema.columns
		WHERE table_schema = DATABASE() AND column_name = 'unit_id'`).Scan(&tables).Error; err != nil {
		t.Fatalf("information_schema: %v", err)
	}
	guarded := map[string]bool{}
	for _, tbl := range project.UnitUsageTables() {
		guarded[tbl] = true
	}
	var missing []string
	for _, tbl := range tables {
		if !guarded[tbl] {
			missing = append(missing, tbl)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("tabel dengan unit_id belum dijaga DeleteUnitIfUnused: %v", missing)
	}
}
