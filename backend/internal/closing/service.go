package closing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

// Service mengorkestrasi completion + true-up. Pola sale.GORMRepository:
// SQL-orchestration langsung, seluruh operasi uang atomik dalam satu tx.
type Service struct {
	db        *gorm.DB
	allocRepo *allocation.GORMRepository // unit inputs (atribut basis)
}

func NewService(db *gorm.DB) *Service {
	return &Service{db: db, allocRepo: allocation.NewGORMRepository(db)}
}

// ── Completion lifecycle (Req #3) ─────────────────────────────────────────────

// MarkCompleted menandai konstruksi selesai (draft→completed; buat event bila
// belum ada). Efek: projects.status='completed' (proyeksi metadata).
func (s *Service) MarkCompleted(ctx context.Context, tenantID, projectID uint64, completedAt time.Time, actorID *uint64) (*CompletionEvent, error) {
	var n int64
	if err := s.db.WithContext(ctx).Table("projects").
		Where("id = ? AND tenant_id = ?", projectID, tenantID).Count(&n).Error; err != nil {
		return nil, fmt.Errorf("cek proyek: %w", err)
	}
	if n == 0 {
		return nil, ErrProjectNotFound
	}

	var ev CompletionEvent
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ferr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND project_id = ? AND phase_id IS NULL", tenantID, projectID).
			First(&ev).Error
		switch {
		case errors.Is(ferr, gorm.ErrRecordNotFound):
			ev = CompletionEvent{
				TenantID: tenantID, ProjectID: projectID,
				Status: CompletionCompleted, CompletedAt: &completedAt,
				CompletedBy: actorID, CreatedBy: actorID,
			}
			if err := tx.Create(&ev).Error; err != nil {
				return fmt.Errorf("buat completion event: %w", err)
			}
		case ferr != nil:
			return fmt.Errorf("baca completion: %w", ferr)
		default:
			if !ev.Status.CanTransitionTo(CompletionCompleted) {
				return fmt.Errorf("%w: completion %s → completed", ErrInvalidStateTransition, ev.Status)
			}
			if err := tx.Model(&CompletionEvent{}).Where("id = ?", ev.ID).Updates(map[string]interface{}{
				"status": string(CompletionCompleted), "completed_at": &completedAt, "completed_by": actorID,
			}).Error; err != nil {
				return fmt.Errorf("update completion: %w", err)
			}
			ev.Status, ev.CompletedAt, ev.CompletedBy = CompletionCompleted, &completedAt, actorID
		}
		// Proyeksi status proyek (metadata; angka tetap dari ledger).
		return tx.Table("projects").
			Where("id = ? AND tenant_id = ?", projectID, tenantID).
			Update("status", "completed").Error
	})
	if err != nil {
		return nil, err
	}
	return &ev, nil
}

// FinalizeCompletion mengunci biaya aktual (completed→finalized) → membuka true-up.
func (s *Service) FinalizeCompletion(ctx context.Context, tenantID, projectID uint64, actorID *uint64) (*CompletionEvent, error) {
	ev, err := s.GetCompletion(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	if !ev.Status.CanTransitionTo(CompletionFinalized) {
		return nil, fmt.Errorf("%w: completion %s → finalized", ErrInvalidStateTransition, ev.Status)
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&CompletionEvent{}).
		Where("id = ? AND tenant_id = ? AND status = ?", ev.ID, tenantID, string(CompletionCompleted)).
		Updates(map[string]interface{}{"status": string(CompletionFinalized), "actual_cost_finalized_at": &now})
	if res.Error != nil {
		return nil, fmt.Errorf("finalize completion: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, fmt.Errorf("%w: completion berubah bersamaan", ErrInvalidStateTransition)
	}
	ev.Status, ev.ActualCostFinalizedAt = CompletionFinalized, &now
	return ev, nil
}

func (s *Service) GetCompletion(ctx context.Context, tenantID, projectID uint64) (*CompletionEvent, error) {
	var ev CompletionEvent
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ? AND phase_id IS NULL", tenantID, projectID).
		First(&ev).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCompletionNotFound
		}
		return nil, fmt.Errorf("baca completion: %w", err)
	}
	return &ev, nil
}

// ── Variance engine (dipakai Preview & Calculate) ─────────────────────────────

type varianceResult struct {
	basis           allocation.AllocationBasis
	configVersionID *uint64
	lines           []TrueupLine
	budgetTotal     domain.Money
	actualTotal     domain.Money
	varianceTotal   domain.Money
	soldUnits       int
	unsoldUnits     int
}

// setAmount menulis nilai satu class ke breakdown (taxonomy-driven).
func setAmount(b *domain.UnitCostBreakdown, c domain.CostCategory, m domain.Money) {
	switch c {
	case domain.CostCategoryLand:
		b.Land = m
	case domain.CostCategoryHard:
		b.Hard = m
	case domain.CostCategorySoft:
		b.Soft = m
	case domain.CostCategoryFinancing:
		b.Financing = m
	}
}

// actualCapitalizedCost menghitung A_c per class dari ledger (posted-only,
// Decision A): Σ debit non-reversal − Σ credit dari jurnal reversal, pada akun
// persediaan 1-3xxx ber-tag project. Kredit BAST (relief HPP) TIDAK mengurangi
// A_c (bukan reversal) — A_c = total biaya aktual dikapitalisasi.
func (s *Service) actualCapitalizedCost(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	// S5: konsumsi pembaca KANONIK ledger.ActualCostByCode (netting tunggal:
	// debit non-reversal − kredit reversal) — SQL netting tidak ditulis ulang.
	codeToClass := map[string]domain.CostCategory{}
	codes := make([]string, 0, len(domain.AllCostCategories))
	for _, c := range domain.AllCostCategories {
		codeToClass[c.InventoryAccountCode()] = c
		codes = append(codes, c.InventoryAccountCode())
	}
	byCode, err := ledger.NewQueryService(s.db).ActualCostByCode(ctx, tenantID, codes,
		ledger.ActualCostScope{ProjectID: projectID})
	if err != nil {
		return domain.UnitCostBreakdown{}, fmt.Errorf("hitung A_c: %w", err)
	}
	var out domain.UnitCostBreakdown
	for code, m := range byCode {
		setAmount(&out, codeToClass[code], m)
	}
	return out, nil
}

// computeVariance menjalankan inti P0-4 §2: act_HPP(u,c) = alokasi A_c ke SEMUA
// unit via basis beku (largest-remainder, Invariant #3); Δ = act − budg (sold).
func (s *Service) computeVariance(ctx context.Context, tenantID, projectID uint64) (*varianceResult, error) {
	// 1. Snapshot terjual (scope IMPL-2: hanya BAST budgeted yang bersnapshot).
	var snaps []sale.AllocationSnapshot
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Find(&snaps).Error; err != nil {
		return nil, fmt.Errorf("baca snapshots: %w", err)
	}
	if len(snaps) == 0 {
		return nil, ErrNoSoldUnits
	}

	// 2. Guard IMPL-1: satu version basis untuk seluruh scope.
	var pinnedID *uint64
	for i := range snaps {
		v := snaps[i].AllocationConfigVersionID
		if v == nil {
			continue // snapshot pra-P0-4 (legacy) — mengikuti version ter-pin lain / aktif
		}
		if pinnedID == nil {
			pinnedID = v
		} else if *pinnedID != *v {
			return nil, ErrMixedAllocationBasisVersion
		}
	}

	// 3. Basis dari version ter-pin (D1) — atau config aktif (semua legacy).
	var basis allocation.AllocationBasis
	if pinnedID != nil {
		var ver allocation.AllocationConfigVersion
		if err := s.db.WithContext(ctx).
			Where("id = ? AND tenant_id = ?", *pinnedID, tenantID).
			First(&ver).Error; err != nil {
			return nil, fmt.Errorf("baca version basis %d: %w", *pinnedID, err)
		}
		basis = ver.Basis
	} else {
		var cfg allocation.AllocationConfig
		if err := s.db.WithContext(ctx).
			Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
			First(&cfg).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrNoBasisConfigured
			}
			return nil, fmt.Errorf("baca basis: %w", err)
		}
		basis = cfg.Basis
	}

	// 4. Budgeted per (unit, class) dari snapshot lines.
	snapByUnit := map[uint64]*sale.AllocationSnapshot{}
	snapIDs := make([]uint64, 0, len(snaps))
	for i := range snaps {
		snapByUnit[snaps[i].UnitID] = &snaps[i]
		snapIDs = append(snapIDs, snaps[i].ID)
	}
	var snapLines []sale.AllocationSnapshotLine
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND snapshot_id IN ?", tenantID, snapIDs).
		Find(&snapLines).Error; err != nil {
		return nil, fmt.Errorf("baca snapshot lines: %w", err)
	}
	snapIDToUnit := map[uint64]uint64{}
	for i := range snaps {
		snapIDToUnit[snaps[i].ID] = snaps[i].UnitID
	}
	budg := map[uint64]map[string]domain.Money{} // unit → class → amount
	histName := map[uint64]string{}              // unit → nama historis (dari snapshot)
	for _, l := range snapLines {
		uid := snapIDToUnit[l.SnapshotID]
		if budg[uid] == nil {
			budg[uid] = map[string]domain.Money{}
		}
		budg[uid][l.AccountingClass] = l.Amount
		if l.UnitNameSnapshot != "" {
			histName[uid] = l.UnitNameSnapshot
		}
	}

	// 5. A_c + alokasi ke semua unit (largest-remainder — engine existing).
	actual, err := s.actualCapitalizedCost(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	units, err := s.allocRepo.GetUnitInputs(ctx, tenantID, projectID)
	if err != nil {
		return nil, fmt.Errorf("ambil atribut unit: %w", err)
	}
	results, err := allocation.Compute(actual, units, basis)
	if err != nil {
		return nil, fmt.Errorf("alokasi act_HPP: %w", err)
	}

	// 6. Nama unit saat ini (fallback bila snapshot tanpa nama historis / unsold).
	var unitRows []struct {
		ID   uint64
		Code string
	}
	if err := s.db.WithContext(ctx).Table("units").Select("id, code").
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Scan(&unitRows).Error; err != nil {
		return nil, fmt.Errorf("baca unit: %w", err)
	}
	curName := map[uint64]string{}
	for _, u := range unitRows {
		curName[u.ID] = u.Code
	}

	// 7. Rakit lines + totals.
	out := &varianceResult{basis: basis, configVersionID: pinnedID}
	zero := domain.FromInt(0)
	for c := range domain.AllCostCategories {
		_ = c
	}
	sold := map[uint64]bool{}
	for _, res := range results {
		snap, isSold := snapByUnit[res.UnitID]
		if isSold {
			sold[res.UnitID] = true
		}
		name := histName[res.UnitID]
		if name == "" {
			name = curName[res.UnitID]
		}
		for _, class := range domain.AllCostCategories {
			act := res.Allocated.Amount(class)
			b := zero
			if isSold {
				b = budg[res.UnitID][string(class)]
			}
			variance := zero
			if isSold {
				variance = act.Sub(b)
			}
			if act.IsZero() && b.IsZero() {
				continue // baris nol murni = noise
			}
			line := TrueupLine{
				TenantID: tenantID, UnitID: res.UnitID, UnitNameSnapshot: name,
				Category: string(class), IsSold: isSold,
				BudgetedAmount: b, ActualAmount: act, VarianceAmount: variance,
			}
			if isSold {
				sid := snap.ID
				line.SnapshotID = &sid
				out.budgetTotal = out.budgetTotal.Add(b)
				out.varianceTotal = out.varianceTotal.Add(variance)
			}
			out.lines = append(out.lines, line)
		}
	}
	out.actualTotal = actual.Total()
	out.soldUnits = len(sold)
	out.unsoldUnits = len(results) - len(sold)
	return out, nil
}

// PreviewVariance — GET hpp-variance (Req #4, read-only; boleh sebelum finalized).
func (s *Service) PreviewVariance(ctx context.Context, tenantID, projectID uint64) (*VariancePreview, error) {
	v, err := s.computeVariance(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}
	status := CompletionStatus("")
	if ev, cerr := s.GetCompletion(ctx, tenantID, projectID); cerr == nil {
		status = ev.Status
	}
	return &VariancePreview{
		ProjectID: projectID, CompletionStatus: status,
		Basis: string(v.basis), ConfigVersionID: v.configVersionID,
		BudgetHPPTotal: v.budgetTotal, ActualCostTotal: v.actualTotal, VarianceTotal: v.varianceTotal,
		SoldUnits: v.soldUnits, UnsoldUnits: v.unsoldUnits, Lines: v.lines,
	}, nil
}

// ── True-up run lifecycle ─────────────────────────────────────────────────────

// Calculate: hitung variance + simpan lines (TANPA jurnal). Gate: completion
// FINALIZED (IMPL-3). Recalculate hanya saat run draft/calculated.
func (s *Service) Calculate(ctx context.Context, tenantID, projectID uint64, actorID *uint64) (*TrueupRun, error) {
	ev, err := s.GetCompletion(ctx, tenantID, projectID)
	if err != nil {
		if errors.Is(err, ErrCompletionNotFound) {
			return nil, ErrCompletionNotFinalized
		}
		return nil, err
	}
	if ev.Status != CompletionFinalized {
		return nil, ErrCompletionNotFinalized
	}

	v, err := s.computeVariance(ctx, tenantID, projectID)
	if err != nil {
		return nil, err
	}

	var run TrueupRun
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		ferr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("tenant_id = ? AND project_id = ? AND status <> 'cancelled'", tenantID, projectID).
			First(&run).Error
		switch {
		case errors.Is(ferr, gorm.ErrRecordNotFound):
			run = TrueupRun{TenantID: tenantID, ProjectID: projectID, Status: RunDraft, CreatedBy: actorID}
			if err := tx.Create(&run).Error; err != nil {
				return fmt.Errorf("buat run: %w", err)
			}
		case ferr != nil:
			return fmt.Errorf("baca run: %w", ferr)
		}
		if !run.Status.CanTransitionTo(RunCalculated) {
			return fmt.Errorf("%w: run %s → calculated", ErrInvalidStateTransition, run.Status)
		}
		// Replace lines (run masih pra-approval; lines belum dokumen final).
		if err := tx.Where("tenant_id = ? AND run_id = ?", tenantID, run.ID).
			Delete(&TrueupLine{}).Error; err != nil {
			return fmt.Errorf("hapus lines lama: %w", err)
		}
		for i := range v.lines {
			v.lines[i].RunID = run.ID
		}
		if len(v.lines) > 0 {
			if err := tx.Create(&v.lines).Error; err != nil {
				return fmt.Errorf("simpan lines: %w", err)
			}
		}
		now := time.Now()
		if err := tx.Model(&TrueupRun{}).Where("id = ?", run.ID).Updates(map[string]interface{}{
			"status": string(RunCalculated), "calculated_at": &now, "calculated_by": actorID,
			"allocation_config_version_id": v.configVersionID,
			"budget_hpp_total":             v.budgetTotal.Decimal(),
			"actual_cost_total":            v.actualTotal.Decimal(),
			"variance_total":               v.varianceTotal.Decimal(),
		}).Error; err != nil {
			return fmt.Errorf("update run calculated: %w", err)
		}
		run.Status = RunCalculated
		run.CalculatedAt, run.CalculatedBy = &now, actorID
		run.AllocationConfigVersionID = v.configVersionID
		run.BudgetHPPTotal, run.ActualCostTotal, run.VarianceTotal = v.budgetTotal, v.actualTotal, v.varianceTotal
		run.Lines = v.lines
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &run, nil
}

// Approve: calculated → approved (pinned; IMPL-5 audit).
func (s *Service) Approve(ctx context.Context, tenantID, runID uint64, actorID *uint64) (*TrueupRun, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&TrueupRun{}).
		Where("id = ? AND tenant_id = ? AND status = ?", runID, tenantID, string(RunCalculated)).
		Updates(map[string]interface{}{"status": string(RunApproved), "approved_at": &now, "approved_by": actorID})
	if res.Error != nil {
		return nil, fmt.Errorf("approve run: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, s.runTransitionErr(ctx, tenantID, runID, RunApproved)
	}
	return s.GetRun(ctx, tenantID, runID)
}

// Post: approved → posted + SATU jurnal adjustment (IMPL-4 idempoten 3 lapis:
// row-lock, journal_id UNIQUE, transisi pinned). Δ>0: Dr HPP/Cr Persediaan;
// Δ<0: kebalikan. Unsold: tanpa jurnal (act_HPP jadi finalized — D2).
// D4: jurnal jatuh di postDate (periode berjalan); period-checker menjaga.
func (s *Service) Post(ctx context.Context, tenantID, runID uint64, postDate time.Time, actorID *uint64) (*TrueupRun, error) {
	var out *TrueupRun
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var run TrueupRun
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", runID, tenantID).
			First(&run).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrRunNotFound
			}
			return fmt.Errorf("lock run: %w", err)
		}
		if run.Status != RunApproved {
			return fmt.Errorf("%w: run %s → posted (hanya approved yang bisa posting)", ErrInvalidStateTransition, run.Status)
		}
		if run.JournalID != nil {
			return fmt.Errorf("%w: run sudah punya jurnal", ErrInvalidStateTransition)
		}

		var lines []TrueupLine
		if err := tx.Where("tenant_id = ? AND run_id = ?", tenantID, run.ID).
			Order("id ASC").Find(&lines).Error; err != nil {
			return fmt.Errorf("baca lines: %w", err)
		}

		// Resolve akun: 5-1000 + persediaan per class.
		accID := map[string]uint64{}
		codes := []string{"5-1000"}
		for _, c := range domain.AllCostCategories {
			codes = append(codes, c.InventoryAccountCode())
		}
		var accRows []struct {
			ID   uint64
			Code string
		}
		if err := tx.Table("accounts").Select("id, code").
			Where("tenant_id = ? AND code IN ?", tenantID, codes).
			Scan(&accRows).Error; err != nil {
			return fmt.Errorf("resolve akun: %w", err)
		}
		for _, a := range accRows {
			accID[a.Code] = a.ID
		}
		if accID["5-1000"] == 0 {
			return fmt.Errorf("akun HPP 5-1000 tidak ditemukan")
		}

		// Rakit jurnal: satu pasang baris per (unit, class) dengan Δ≠0 (sold).
		var jl []ledger.LineInput
		classCode := map[string]string{}
		for _, c := range domain.AllCostCategories {
			classCode[string(c)] = c.InventoryAccountCode()
		}
		pid := run.ProjectID
		adjusted := make([]int, 0, len(lines)) // indeks line yang dijurnal
		for i, l := range lines {
			if !l.IsSold || l.VarianceAmount.IsZero() {
				continue
			}
			invID := accID[classCode[l.Category]]
			if invID == 0 {
				return fmt.Errorf("akun persediaan class %s tidak ditemukan", l.Category)
			}
			uid := l.UnitID
			v := l.VarianceAmount
			if v.IsNeg() {
				abs := v.Neg()
				jl = append(jl,
					ledger.LineInput{AccountID: invID, Debit: abs, ProjectID: &pid, UnitID: &uid, Description: fmt.Sprintf("True-up %s %s (Δ<0)", l.UnitNameSnapshot, l.Category)},
					ledger.LineInput{AccountID: accID["5-1000"], Credit: abs, ProjectID: &pid, UnitID: &uid, Description: fmt.Sprintf("Koreksi HPP %s %s", l.UnitNameSnapshot, l.Category)},
				)
			} else {
				jl = append(jl,
					ledger.LineInput{AccountID: accID["5-1000"], Debit: v, ProjectID: &pid, UnitID: &uid, Description: fmt.Sprintf("True-up HPP %s %s (Δ>0)", l.UnitNameSnapshot, l.Category)},
					ledger.LineInput{AccountID: invID, Credit: v, ProjectID: &pid, UnitID: &uid, Description: fmt.Sprintf("Relief persediaan %s %s", l.UnitNameSnapshot, l.Category)},
				)
			}
			adjusted = append(adjusted, i)
		}

		var journalID *uint64
		if len(jl) > 0 {
			txLedgerRepo := ledger.NewGORMRepository(tx)
			txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)
			entry, jerr := txPosting.Create(ctx, ledger.CreateJournalRequest{
				TenantID:    tenantID,
				Date:        postDate,
				Description: fmt.Sprintf("HPP True-up proyek %d (run #%d) — prior period HPP adjustment", run.ProjectID, run.ID),
				Reference:   fmt.Sprintf("HPP-TRUEUP-%d", run.ID),
				Source:      "hpp_trueup",
				CreatedBy:   actorID,
				Lines:       jl,
			})
			if jerr != nil {
				return fmt.Errorf("buat jurnal true-up: %w", jerr)
			}
			if _, jerr := txPosting.Post(ctx, tenantID, entry.ID); jerr != nil {
				return fmt.Errorf("posting jurnal true-up: %w", jerr)
			}
			journalID = &entry.ID
			// Tandai lines yang ter-jurnal.
			ids := make([]uint64, 0, len(adjusted))
			for _, i := range adjusted {
				ids = append(ids, lines[i].ID)
			}
			if err := tx.Model(&TrueupLine{}).
				Where("tenant_id = ? AND id IN ?", tenantID, ids).
				Update("journal_entry_id", entry.ID).Error; err != nil {
				return fmt.Errorf("tautkan lines ke jurnal: %w", err)
			}
		}

		now := time.Now()
		res := tx.Model(&TrueupRun{}).
			Where("id = ? AND tenant_id = ? AND status = ?", run.ID, tenantID, string(RunApproved)).
			Updates(map[string]interface{}{
				"status": string(RunPosted), "posted_at": &now, "posted_by": actorID, "journal_id": journalID,
			})
		if res.Error != nil {
			return fmt.Errorf("update run posted: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: run berubah bersamaan", ErrInvalidStateTransition)
		}
		run.Status, run.PostedAt, run.PostedBy, run.JournalID = RunPosted, &now, actorID, journalID
		run.Lines = lines
		out = &run
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Cancel: non-terminal → cancelled (audit; active_scope terbebas utk run baru).
func (s *Service) Cancel(ctx context.Context, tenantID, runID uint64, actorID *uint64) (*TrueupRun, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&TrueupRun{}).
		Where("id = ? AND tenant_id = ? AND status IN ?", runID, tenantID,
			[]string{string(RunDraft), string(RunCalculated), string(RunApproved)}).
		Updates(map[string]interface{}{"status": string(RunCancelled), "cancelled_at": &now, "cancelled_by": actorID})
	if res.Error != nil {
		return nil, fmt.Errorf("cancel run: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, s.runTransitionErr(ctx, tenantID, runID, RunCancelled)
	}
	return s.GetRun(ctx, tenantID, runID)
}

func (s *Service) runTransitionErr(ctx context.Context, tenantID, runID uint64, target RunStatus) error {
	r, err := s.GetRun(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: run %s → %s", ErrInvalidStateTransition, r.Status, target)
}

func (s *Service) GetRun(ctx context.Context, tenantID, runID uint64) (*TrueupRun, error) {
	var run TrueupRun
	err := s.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", runID, tenantID).
		First(&run).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrRunNotFound
		}
		return nil, fmt.Errorf("baca run: %w", err)
	}
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND run_id = ?", tenantID, runID).
		Order("id ASC").Find(&run.Lines).Error; err != nil {
		return nil, fmt.Errorf("baca lines: %w", err)
	}
	return &run, nil
}

func (s *Service) ListRuns(ctx context.Context, tenantID, projectID uint64) ([]*TrueupRun, error) {
	var runs []*TrueupRun
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ?", tenantID, projectID).
		Order("id DESC").Find(&runs).Error
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	return runs, nil
}

// ── D2 — sumber HPP finalized untuk BAST pasca-completion ─────────────────────

// FinalizedUnitHPP (kontrak sale.FinalizedHPPSource):
//   - completion belum finalized → (zero, false, nil): jalur BAST normal.
//   - finalized + run posted     → act_HPP finalized unit dari hpp_trueup_lines.
//   - finalized tanpa run posted → ErrTrueupNotPosted (blokir BAST — D2:
//     no live recalculation after completion).
func (s *Service) FinalizedUnitHPP(ctx context.Context, tenantID, projectID, unitID uint64) (domain.UnitCostBreakdown, bool, error) {
	ev, err := s.GetCompletion(ctx, tenantID, projectID)
	if err != nil {
		if errors.Is(err, ErrCompletionNotFound) {
			return domain.UnitCostBreakdown{}, false, nil
		}
		return domain.UnitCostBreakdown{}, false, err
	}
	if ev.Status != CompletionFinalized {
		return domain.UnitCostBreakdown{}, false, nil
	}
	var run TrueupRun
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND project_id = ? AND status = ?", tenantID, projectID, string(RunPosted)).
		First(&run).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.UnitCostBreakdown{}, false, ErrTrueupNotPosted
		}
		return domain.UnitCostBreakdown{}, false, fmt.Errorf("baca run posted: %w", err)
	}
	var lines []TrueupLine
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND run_id = ? AND unit_id = ?", tenantID, run.ID, unitID).
		Find(&lines).Error; err != nil {
		return domain.UnitCostBreakdown{}, false, fmt.Errorf("baca lines unit: %w", err)
	}
	var out domain.UnitCostBreakdown
	for _, l := range lines {
		setAmount(&out, domain.CostCategory(l.Category), l.ActualAmount)
	}
	return out, true, nil
}
