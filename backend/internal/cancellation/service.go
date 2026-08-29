package cancellation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

// Kode akun (COA sistem).
const (
	accUangMuka       = "2-2000"
	accTitipanBooking = "2-2100"
	accHutangRefund   = "2-2200"
	accPendapatanLain = "4-2000"
)

// ApprovalGate — seam opt-in ke Generic Approval Workflow (TargetCancellation).
// nil / tanpa workflow aktif = tanpa gate (pola budget/project).
type ApprovalGate interface {
	RequireApproved(ctx context.Context, tenantID, cancellationID uint64) error
}

// Service mengorkestrasi cancellation + refund (pola sale/closing: SQL-orchestration,
// seluruh operasi uang atomik satu tx).
type Service struct {
	db           *gorm.DB
	approvalGate ApprovalGate
	realization  RealizationReleaser
}

// RealizationReleaser membalik piutang biaya realisasi sebuah unit di dalam
// transaksi pembatalan (W-5). Diimplementasi charge.Service; dipasang di main.go
// lewat interface agar paket ini tidak meng-import charge.
//
// Dana yang sudah diterima TIDAK disentuh olehnya — disposisinya tetap keputusan
// admin (R-4) dan sudah ditangani langkah settlement dana buyer di bawah.
type RealizationReleaser interface {
	ReverseRecognitionInTx(ctx context.Context, tx *gorm.DB, tenantID, unitID uint64, createdBy *uint64) error
}

func NewService(db *gorm.DB) *Service { return &Service{db: db} }

// SetApprovalGate memasang gate approval (wiring produksi).
func (s *Service) SetApprovalGate(g ApprovalGate) { s.approvalGate = g }

// SetRealizationReleaser memasang pembalik piutang biaya realisasi (W-5).
func (s *Service) SetRealizationReleaser(r RealizationReleaser) { s.realization = r }

// ── Helpers ───────────────────────────────────────────────────────────────────

type accountRef struct {
	ID   uint64
	Name string
}

func (s *Service) accountsByCode(ctx context.Context, tenantID uint64, codes []string) (map[string]accountRef, error) {
	var rows []struct {
		ID   uint64
		Code string
		Name string
	}
	if err := s.db.WithContext(ctx).Table("accounts").Select("id, code, name").
		Where("tenant_id = ? AND code IN ?", tenantID, codes).
		Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("resolve akun: %w", err)
	}
	out := map[string]accountRef{}
	for _, r := range rows {
		out[r.Code] = accountRef{ID: r.ID, Name: r.Name}
	}
	for _, c := range codes {
		if _, ok := out[c]; !ok {
			return nil, fmt.Errorf("akun %s tidak ditemukan (jalankan migration/seed COA)", c)
		}
	}
	return out, nil
}

type unitRow struct {
	ID        uint64
	ProjectID uint64
	PhaseID   *uint64
	Code      string
	Status    string
	BuyerRef  *string
}

func (s *Service) unit(ctx context.Context, tenantID, unitID uint64) (*unitRow, error) {
	var u unitRow
	err := s.db.WithContext(ctx).Table("units").
		Select("id, project_id, phase_id, code, status, buyer_ref").
		Where("id = ? AND tenant_id = ?", unitID, tenantID).
		Scan(&u).Error
	if err != nil {
		return nil, fmt.Errorf("baca unit: %w", err)
	}
	if u.ID == 0 {
		return nil, ErrUnitNotFound
	}
	return &u, nil
}

// receivedAdvance: Σ(credit − debit) 2-2000 ber-tag unit dari jurnal POSTED —
// posisi dana buyer di Uang Muka (derived; ledger = SoT).
func (s *Service) receivedAdvance(ctx context.Context, tenantID, unitID uint64) (domain.Money, error) {
	var r struct{ Amt string }
	err := s.db.WithContext(ctx).Raw(`
		SELECT COALESCE(SUM(jl.credit - jl.debit), 0) AS amt
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL
		  AND a.code = ? AND jl.unit_id = ?`, tenantID, accUangMuka, unitID).Scan(&r).Error
	if err != nil {
		return domain.Zero, fmt.Errorf("hitung uang muka unit: %w", err)
	}
	return domain.NewMoney(r.Amt)
}

// journalLinesWithCode membaca baris jurnal (+kode/nama akun) — bahan mirror.
type journalLineRow struct {
	AccountID   uint64
	Code        string
	Name        string
	Debit       string
	Credit      string
	ProjectID   *uint64
	PhaseID     *uint64
	UnitID      *uint64
	Description string
}

func (s *Service) journalLines(ctx context.Context, tenantID, journalID uint64) ([]journalLineRow, error) {
	var rows []journalLineRow
	err := s.db.WithContext(ctx).Raw(`
		SELECT jl.account_id, a.code, a.name, jl.debit, jl.credit,
		       jl.project_id, jl.phase_id, jl.unit_id, jl.description
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.id = ? AND je.posted_at IS NOT NULL
		ORDER BY jl.id`, tenantID, journalID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("baca baris jurnal %d: %w", journalID, err)
	}
	return rows, nil
}

// cogsSourceLines: SELURUH baris jurnal COGS unit — Event 4 (cogs_journal_id)
// + baris jurnal true-up (source='hpp_trueup') ber-tag unit. INV-COGS-SUM:
// recognized COGS unit == Σ himpunan ini; pembatalan = mirror himpunan ini.
func (s *Service) cogsSourceLines(ctx context.Context, tenantID, unitID uint64, cogsJournalID *uint64) ([]journalLineRow, error) {
	var rows []journalLineRow
	cj := uint64(0)
	if cogsJournalID != nil {
		cj = *cogsJournalID
	}
	err := s.db.WithContext(ctx).Raw(`
		SELECT jl.account_id, a.code, a.name, jl.debit, jl.credit,
		       jl.project_id, jl.phase_id, jl.unit_id, jl.description
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL
		  AND ( je.id = ?
		        OR (je.source = 'hpp_trueup' AND jl.unit_id = ?) )
		ORDER BY jl.id`, tenantID, cj, unitID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("baca jurnal COGS unit: %w", err)
	}
	return rows, nil
}

// bundledLandSale finds the land_sales row (status=akad) for a contract's
// bundled Kelebihan Tanah component (kelebihan-tanah-booking-integration-2026-08),
// if any. Returns nil, nil when the contract has no land component or was
// created before this feature (land_reservation_id NULL) — normal path for
// the vast majority of contracts.
func (s *Service) bundledLandSale(ctx context.Context, tenantID, contractID uint64) (*land.LandSale, error) {
	var reservationID *uint64
	if err := s.db.WithContext(ctx).Table("sale_contracts").
		Select("land_reservation_id").
		Where("id = ? AND tenant_id = ?", contractID, tenantID).
		Scan(&reservationID).Error; err != nil {
		return nil, fmt.Errorf("baca komponen tanah kontrak: %w", err)
	}
	if reservationID == nil {
		return nil, nil
	}
	var ls land.LandSale
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND reservation_id = ? AND status = ?", tenantID, *reservationID, land.LandSaleStatusAkad).
		First(&ls).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("baca land_sale komponen tanah: %w", err)
	}
	return &ls, nil
}

func mirror(rows []journalLineRow) ([]ledger.LineInput, []PreviewLine, error) {
	var lines []ledger.LineInput
	var prev []PreviewLine
	for _, r := range rows {
		d, err := domain.NewMoney(r.Debit)
		if err != nil {
			return nil, nil, err
		}
		c, err := domain.NewMoney(r.Credit)
		if err != nil {
			return nil, nil, err
		}
		// Swap Dr/Cr (jurnal pembalik).
		lines = append(lines, ledger.LineInput{
			AccountID: r.AccountID, Debit: c, Credit: d,
			ProjectID: r.ProjectID, PhaseID: r.PhaseID, UnitID: r.UnitID,
			Description: "Pembalikan: " + r.Description,
		})
		prev = append(prev, PreviewLine{AccountCode: r.Code, AccountName: r.Name, Debit: c, Credit: d})
	}
	return lines, prev, nil
}

// ── Request / Approve / Reject ────────────────────────────────────────────────

type RequestInput struct {
	UnitID    uint64
	Reason    string
	Penalty   domain.Money
	EventDate time.Time
	ActorID   *uint64
}

type saleRecordRow struct {
	ID               uint64
	RevenueJournalID uint64
	COGSJournalID    *uint64
	CancelledAt      *time.Time
}

func (s *Service) activeSaleRecord(ctx context.Context, tenantID, unitID uint64) (*saleRecordRow, error) {
	var r saleRecordRow
	err := s.db.WithContext(ctx).Table("sale_records").
		Select("id, revenue_journal_id, cogs_journal_id, cancelled_at").
		Where("tenant_id = ? AND unit_id = ? AND cancelled_at IS NULL", tenantID, unitID).
		Scan(&r).Error
	if err != nil {
		return nil, fmt.Errorf("baca sale record: %w", err)
	}
	if r.ID == 0 {
		return nil, nil
	}
	return &r, nil
}

// Request membuat dokumen cancellation (deteksi stage otomatis).
func (s *Service) Request(ctx context.Context, tenantID uint64, in RequestInput) (*Cancellation, error) {
	if in.Penalty.IsNeg() || !in.Penalty.IsWholeRupiah() {
		return nil, ErrPenaltyInvalid
	}
	u, err := s.unit(ctx, tenantID, in.UnitID)
	if err != nil {
		return nil, err
	}
	rec, err := s.activeSaleRecord(ctx, tenantID, in.UnitID)
	if err != nil {
		return nil, err
	}

	var stage Stage
	switch {
	case rec != nil:
		if u.Status != string(project.UnitStatusSold) {
			return nil, fmt.Errorf("%w: unit ber-BAST tapi status %s", ErrUnitStageInvalid, u.Status)
		}
		stage = StagePostBAST
	case u.Status == string(project.UnitStatusReserved) || u.Status == string(project.UnitStatusPPJB):
		stage = StagePreBAST
	case u.Status == string(project.UnitStatusBooked):
		return nil, fmt.Errorf("%w: unit booked — gunakan pembatalan booking", ErrUnitStageInvalid)
	default:
		return nil, fmt.Errorf("%w: status unit %s", ErrUnitStageInvalid, u.Status)
	}

	received, err := s.receivedAdvance(ctx, tenantID, in.UnitID)
	if err != nil {
		return nil, err
	}
	if stage == StagePreBAST && received.IsZero() {
		return nil, ErrNothingToCancel
	}

	// Kontrak (opsional; legacy unit-only tetap didukung).
	var contractID *uint64
	var cid uint64
	if err := s.db.WithContext(ctx).Table("sale_contracts").Select("id").
		Where("tenant_id = ? AND unit_id = ?", tenantID, in.UnitID).
		Order("id DESC").Limit(1).Scan(&cid).Error; err != nil {
		return nil, fmt.Errorf("baca kontrak: %w", err)
	}
	if cid != 0 {
		contractID = &cid
	}

	y := "Y"
	c := &Cancellation{
		TenantID: tenantID, ProjectID: u.ProjectID, UnitID: in.UnitID,
		SaleContractID: contractID, Stage: stage,
		Reason: in.Reason, EventDate: in.EventDate, Penalty: in.Penalty,
		ReceivedTotal: received, // indikatif; dihitung ulang otoritatif saat process
		Status:        StatusRequested, RequestedBy: in.ActorID, ActiveKey: &y,
	}
	if rec != nil {
		c.SaleRecordID = &rec.ID
	}
	if err := s.db.WithContext(ctx).Create(c).Error; err != nil {
		if isDuplicate(err) {
			return nil, ErrActiveCancellationExists
		}
		return nil, fmt.Errorf("simpan cancellation: %w", err)
	}
	return c, nil
}

func isDuplicate(err error) bool {
	return err != nil && (errors.Is(err, gorm.ErrDuplicatedKey) ||
		containsStr(err.Error(), "Duplicate entry"))
}

func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// Approve: requested → approved. Gate opt-in Generic Approval Workflow.
func (s *Service) Approve(ctx context.Context, tenantID, id uint64, actorID *uint64) (*Cancellation, error) {
	if s.approvalGate != nil {
		if err := s.approvalGate.RequireApproved(ctx, tenantID, id); err != nil {
			return nil, err
		}
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Cancellation{}).
		Where("id = ? AND tenant_id = ? AND status = ?", id, tenantID, string(StatusRequested)).
		Updates(map[string]interface{}{"status": string(StatusApproved), "approved_at": &now, "approved_by": actorID})
	if res.Error != nil {
		return nil, fmt.Errorf("approve: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, s.transitionErr(ctx, tenantID, id, StatusApproved)
	}
	return s.Get(ctx, tenantID, id)
}

// Reject: requested|approved → rejected (active_key dilepas — unit bebas lagi).
func (s *Service) Reject(ctx context.Context, tenantID, id uint64, reason string, actorID *uint64) (*Cancellation, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Cancellation{}).
		Where("id = ? AND tenant_id = ? AND status IN ?", id, tenantID,
			[]string{string(StatusRequested), string(StatusApproved)}).
		Updates(map[string]interface{}{
			"status": string(StatusRejected), "rejected_at": &now, "rejected_by": actorID,
			"reject_reason": reason, "active_key": nil,
		})
	if res.Error != nil {
		return nil, fmt.Errorf("reject: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return nil, s.transitionErr(ctx, tenantID, id, StatusRejected)
	}
	return s.Get(ctx, tenantID, id)
}

func (s *Service) transitionErr(ctx context.Context, tenantID, id uint64, target Status) error {
	c, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("%w: %s → %s", ErrInvalidStateTransition, c.Status, target)
}

// ── Plan (dipakai Preview & Process — satu sumber kebenaran) ──────────────────

type processPlan struct {
	preview       ProcessPreview
	received      domain.Money
	refundNet     domain.Money
	cogsLines     []ledger.LineInput
	settleLines   []ledger.LineInput
	revJournalID  uint64   // Event 3 (post_bast)
	taxJournalIDs []uint64 // akrual PPh yang akan di-reverse
	obligationIDs []uint64
	fromStatus    project.UnitStatus
	unitEvent     project.TransitionEvent
	payee         string
	// landSaleID: komponen Kelebihan Tanah bundled pada kontrak unit ini, bila
	// ada (kelebihan-tanah-booking-integration-2026-08) — dibalik via
	// land.CancelLandSaleTx di dalam tx Process yang sama.
	landSaleID *uint64
}

func (s *Service) buildPlan(ctx context.Context, tenantID uint64, c *Cancellation) (*processPlan, error) {
	u, err := s.unit(ctx, tenantID, c.UnitID)
	if err != nil {
		return nil, err
	}
	accs, err := s.accountsByCode(ctx, tenantID, []string{accUangMuka, accPendapatanLain, accHutangRefund})
	if err != nil {
		return nil, err
	}

	plan := &processPlan{preview: ProcessPreview{
		CancellationID: c.ID, Stage: c.Stage, Penalty: c.Penalty, UnitNextStatus: "available",
	}}
	plan.payee = ""
	if u.BuyerRef != nil {
		plan.payee = *u.BuyerRef
	}
	if c.SaleContractID != nil {
		var bn string
		s.db.WithContext(ctx).Table("sale_contracts").Select("buyer_name").
			Where("id = ?", *c.SaleContractID).Scan(&bn)
		if bn != "" {
			plan.payee = bn
		}
	}

	received, err := s.receivedAdvance(ctx, tenantID, c.UnitID)
	if err != nil {
		return nil, err
	}

	switch c.Stage {
	case StagePreBAST:
		switch u.Status {
		case string(project.UnitStatusReserved):
			plan.fromStatus, plan.unitEvent = project.UnitStatusReserved, project.EventReservationCancelled
		case string(project.UnitStatusPPJB):
			plan.fromStatus, plan.unitEvent = project.UnitStatusPPJB, project.EventContractCancelled
		default:
			return nil, fmt.Errorf("%w: status unit %s", ErrUnitStageInvalid, u.Status)
		}

	case StagePostBAST:
		if u.Status != string(project.UnitStatusSold) || c.SaleRecordID == nil {
			return nil, fmt.Errorf("%w: status unit %s", ErrUnitStageInvalid, u.Status)
		}
		plan.fromStatus, plan.unitEvent = project.UnitStatusSold, project.EventCancelledPostBAST

		var rec saleRecordRow
		if err := s.db.WithContext(ctx).Table("sale_records").
			Select("id, revenue_journal_id, cogs_journal_id, cancelled_at").
			Where("id = ? AND tenant_id = ?", *c.SaleRecordID, tenantID).
			Scan(&rec).Error; err != nil || rec.ID == 0 {
			return nil, fmt.Errorf("baca sale record: %w", err)
		}
		plan.revJournalID = rec.RevenueJournalID

		// Revenue reversal (mirror Event 3) — preview + dana kembali ke Uang Muka.
		revRows, err := s.journalLines(ctx, tenantID, rec.RevenueJournalID)
		if err != nil {
			return nil, err
		}
		_, revPrev, err := mirror(revRows)
		if err != nil {
			return nil, err
		}
		plan.preview.Journals = append(plan.preview.Journals, PreviewJournal{
			Purpose: "revenue_reversal", Label: "Pembalikan pengakuan pendapatan (Event 3)", Lines: revPrev,
		})
		for _, r := range revRows {
			// Uang muka yang didebit saat BAST kembali menjadi kewajiban.
			if r.Code == accUangMuka {
				d, _ := domain.NewMoney(r.Debit)
				received = received.Add(d)
			}
			// Total pendapatan yang dibatalkan (kredit 4-xxxx di Event 3).
			if len(r.Code) > 0 && r.Code[0] == '4' {
				cAmt, _ := domain.NewMoney(r.Credit)
				plan.preview.RevenueReversed = plan.preview.RevenueReversed.Add(cAmt)
			}
		}

		// COGS reversal — INV-COGS-SUM: mirror himpunan jurnal COGS unit.
		cogsRows, err := s.cogsSourceLines(ctx, tenantID, c.UnitID, rec.COGSJournalID)
		if err != nil {
			return nil, err
		}
		if len(cogsRows) > 0 {
			lines, prev, err := mirror(cogsRows)
			if err != nil {
				return nil, err
			}
			plan.cogsLines = lines
			plan.preview.Journals = append(plan.preview.Journals, PreviewJournal{
				Purpose: "cogs_reversal", Label: "Pembalikan HPP (Event 4 + porsi true-up)", Lines: prev,
			})
			for _, r := range cogsRows {
				if r.Code == "5-1000" {
					d, _ := domain.NewMoney(r.Debit)
					cr, _ := domain.NewMoney(r.Credit)
					plan.preview.COGSReversed = plan.preview.COGSReversed.Add(d).Sub(cr)
				}
			}
		}

		// Komponen Kelebihan Tanah bundled (kelebihan-tanah-booking-integration-2026-08)
		// — kontrak unit ini mungkin menyertakan penjualan tanah yang di-Akad-kan
		// bersamaan (RecordAkadTx, atomik dalam Event3/4 unit). Bila ada, ikut
		// dibalik di Process (land.CancelLandSaleTx) — bukan engine kedua, mirror
		// pola cogsSourceLines di atas.
		if c.SaleContractID != nil {
			ls, err := s.bundledLandSale(ctx, tenantID, *c.SaleContractID)
			if err != nil {
				return nil, err
			}
			if ls != nil {
				plan.landSaleID = &ls.ID
				if ls.RevenueJournalID != nil {
					revRows, err := s.journalLines(ctx, tenantID, *ls.RevenueJournalID)
					if err != nil {
						return nil, err
					}
					_, prev, err := mirror(revRows)
					if err != nil {
						return nil, err
					}
					plan.preview.Journals = append(plan.preview.Journals, PreviewJournal{
						Purpose: "land_revenue_reversal", Label: "Pembalikan pengakuan pendapatan Kelebihan Tanah (Akad)", Lines: prev,
					})
					for _, r := range revRows {
						if len(r.Code) > 0 && r.Code[0] == '4' {
							cAmt, _ := domain.NewMoney(r.Credit)
							plan.preview.LandRevenueReversed = plan.preview.LandRevenueReversed.Add(cAmt)
						}
					}
				}
				if ls.CogsJournalID != nil {
					cogsRows, err := s.journalLines(ctx, tenantID, *ls.CogsJournalID)
					if err != nil {
						return nil, err
					}
					_, prev, err := mirror(cogsRows)
					if err != nil {
						return nil, err
					}
					plan.preview.Journals = append(plan.preview.Journals, PreviewJournal{
						Purpose: "land_cogs_reversal", Label: "Pembalikan HPP Kelebihan Tanah (Akad)", Lines: prev,
					})
					for _, r := range cogsRows {
						if r.Code == "5-1000" {
							d, _ := domain.NewMoney(r.Debit)
							cr, _ := domain.NewMoney(r.Credit)
							plan.preview.LandCOGSReversed = plan.preview.LandCOGSReversed.Add(d).Sub(cr)
						}
					}
				}
			}
		}

		// PPh Final: obligation unit — paid = blokir; outstanding = reverse akrual.
		var obls []struct {
			ID             uint64
			Status         string
			JournalEntryID uint64
			TaxAmount      string
		}
		if err := s.db.WithContext(ctx).Table("tax_obligations").
			Select("id, status, journal_entry_id, tax_amount").
			Where("tenant_id = ? AND unit_id = ?", tenantID, c.UnitID).
			Scan(&obls).Error; err != nil {
			return nil, fmt.Errorf("baca tax obligations: %w", err)
		}
		for _, o := range obls {
			if o.Status == "paid" {
				return nil, ErrTaxAlreadyPaid
			}
			if o.Status != "outstanding" {
				continue // cancelled dsb.
			}
			plan.taxJournalIDs = append(plan.taxJournalIDs, o.JournalEntryID)
			plan.obligationIDs = append(plan.obligationIDs, o.ID)
			taxRows, err := s.journalLines(ctx, tenantID, o.JournalEntryID)
			if err != nil {
				return nil, err
			}
			_, taxPrev, err := mirror(taxRows)
			if err != nil {
				return nil, err
			}
			plan.preview.Journals = append(plan.preview.Journals, PreviewJournal{
				Purpose: "tax_reversal", Label: "Pembalikan akrual PPh Final (Event 5)", Lines: taxPrev,
			})
			amt, _ := domain.NewMoney(o.TaxAmount)
			plan.preview.TaxReversed = plan.preview.TaxReversed.Add(amt)
		}
	}

	// Settlement (kedua stage): Dr Uang Muka Σ / Cr Penalti / Cr Hutang Refund.
	if c.Penalty.GreaterThan(received) {
		return nil, ErrPenaltyExceedsReceived
	}
	plan.received = received
	plan.refundNet = received.Sub(c.Penalty)
	pid, uid := c.ProjectID, c.UnitID
	if received.GreaterThan(domain.Zero) {
		var prev []PreviewLine
		plan.settleLines = append(plan.settleLines, ledger.LineInput{
			AccountID: accs[accUangMuka].ID, Debit: received, ProjectID: &pid, UnitID: &uid,
			Description: "Pembatalan: pelepasan uang muka buyer",
		})
		prev = append(prev, PreviewLine{AccountCode: accUangMuka, AccountName: accs[accUangMuka].Name, Debit: received})
		if c.Penalty.GreaterThan(domain.Zero) {
			plan.settleLines = append(plan.settleLines, ledger.LineInput{
				AccountID: accs[accPendapatanLain].ID, Credit: c.Penalty, ProjectID: &pid, UnitID: &uid,
				Description: "Penalti pembatalan (forfeit)",
			})
			prev = append(prev, PreviewLine{AccountCode: accPendapatanLain, AccountName: accs[accPendapatanLain].Name, Credit: c.Penalty})
		}
		if plan.refundNet.GreaterThan(domain.Zero) {
			plan.settleLines = append(plan.settleLines, ledger.LineInput{
				AccountID: accs[accHutangRefund].ID, Credit: plan.refundNet, ProjectID: &pid, UnitID: &uid,
				Description: "Hutang refund kepada buyer",
			})
			prev = append(prev, PreviewLine{AccountCode: accHutangRefund, AccountName: accs[accHutangRefund].Name, Credit: plan.refundNet})
		}
		plan.preview.Journals = append(plan.preview.Journals, PreviewJournal{
			Purpose: "settlement", Label: "Penyelesaian dana buyer (penalti + hutang refund)", Lines: prev,
		})
	}
	plan.preview.ReceivedTotal = received
	plan.preview.RefundAmount = plan.refundNet
	return plan, nil
}

// Preview — Journal Preview untuk UI sebelum eksekusi (read-only).
func (s *Service) Preview(ctx context.Context, tenantID, id uint64) (*ProcessPreview, error) {
	c, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if c.Status.Terminal() {
		return nil, fmt.Errorf("%w: cancellation sudah %s", ErrInvalidStateTransition, c.Status)
	}
	plan, err := s.buildPlan(ctx, tenantID, c)
	if err != nil {
		return nil, err
	}
	return &plan.preview, nil
}

// ── Process (eksekusi atomik) ─────────────────────────────────────────────────

func (s *Service) Process(ctx context.Context, tenantID, id uint64, actorID *uint64) (*Cancellation, error) {
	var out *Cancellation
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var c Cancellation
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND tenant_id = ?", id, tenantID).
			First(&c).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return fmt.Errorf("lock cancellation: %w", err)
		}
		if c.Status != StatusApproved {
			return fmt.Errorf("%w: %s → processed (butuh approved)", ErrInvalidStateTransition, c.Status)
		}

		// Rencana dihitung ulang OTORITATIF di dalam tx (dana bisa berubah
		// antara request dan process).
		txSvc := &Service{db: tx}
		plan, err := txSvc.buildPlan(ctx, tenantID, &c)
		if err != nil {
			return err
		}

		txLedgerRepo := ledger.NewGORMRepository(tx)
		txPosting := ledger.NewPostingService(txLedgerRepo, txLedgerRepo).WithPeriodChecker(txLedgerRepo)

		var revID, cogsID, taxID *uint64
		var landRevID, landCogsID *uint64

		if c.Stage == StagePostBAST {
			// 1. Reverse Event 3 (chain ReversesID utuh — audit).
			rev, err := txPosting.Reverse(ctx, tenantID, plan.revJournalID, c.EventDate)
			if err != nil {
				return fmt.Errorf("pembalikan pendapatan: %w", err)
			}
			revID = &rev.ID

			// 2. COGS reversal (INV-COGS-SUM — mirror himpunan).
			if len(plan.cogsLines) > 0 {
				entry, err := txPosting.Create(ctx, ledger.CreateJournalRequest{
					TenantID: tenantID, Date: c.EventDate,
					Description: fmt.Sprintf("Pembatalan unit: pembalikan HPP (cancellation #%d)", c.ID),
					Reference:   fmt.Sprintf("CX-%d-COGS", c.ID),
					Source:      "cancellation", CreatedBy: actorID,
					Lines: plan.cogsLines,
				})
				if err != nil {
					return fmt.Errorf("jurnal pembalikan HPP: %w", err)
				}
				if _, err := txPosting.Post(ctx, tenantID, entry.ID); err != nil {
					return fmt.Errorf("posting pembalikan HPP: %w", err)
				}
				cogsID = &entry.ID
			}

			// 2b. Komponen Kelebihan Tanah bundled (kelebihan-tanah-booking-integration-2026-08)
			//     — reversal + pelepasan sold_quantity_m2 + status cancelled,
			//     satu tx dengan pembalikan Event3/4 unit di atas.
			if plan.landSaleID != nil {
				ls, err := land.CancelLandSaleTx(ctx, tx, tenantID, *plan.landSaleID, land.CancelLandSaleParams{
					Reason:      fmt.Sprintf("Pembatalan unit (cancellation #%d): %s", c.ID, c.Reason),
					CancelledBy: actorID,
					CancelDate:  c.EventDate,
				})
				if err != nil {
					return fmt.Errorf("pembalikan komponen Kelebihan Tanah: %w", err)
				}
				landRevID = ls.RevenueReversalJournalID
				landCogsID = ls.CogsReversalJournalID
			}

			// 3. Reverse akrual PPh Final + void obligation.
			for i, jid := range plan.taxJournalIDs {
				trev, err := txPosting.Reverse(ctx, tenantID, jid, c.EventDate)
				if err != nil {
					return fmt.Errorf("pembalikan PPh: %w", err)
				}
				if i == 0 {
					taxID = &trev.ID
				}
				if err := tx.Table("tax_obligations").
					Where("id = ? AND tenant_id = ? AND status = 'outstanding'", plan.obligationIDs[i], tenantID).
					Update("status", "cancelled").Error; err != nil {
					return fmt.Errorf("void obligation: %w", err)
				}
			}

			// 4. Tandai sale_record (baris BAST tetap — append-only).
			now := time.Now()
			if err := tx.Table("sale_records").
				Where("id = ? AND tenant_id = ? AND cancelled_at IS NULL", *c.SaleRecordID, tenantID).
				Updates(map[string]interface{}{"cancelled_at": &now, "cancellation_id": c.ID}).Error; err != nil {
				return fmt.Errorf("tandai sale record: %w", err)
			}
		}

		// 4b. Piutang biaya realisasi ikut batal (W-5). Tagihan atas unit yang
		//     penjualannya dibatalkan bukan lagi klaim ke customer — meninggalkan
		//     saldo 1-2000 di sini berarti menagih orang atas rumah yang tidak
		//     jadi ia beli.
		if s.realization != nil {
			if err := s.realization.ReverseRecognitionInTx(ctx, tx, tenantID, c.UnitID, actorID); err != nil {
				return fmt.Errorf("pembalikan piutang biaya realisasi: %w", err)
			}
		}

		// 5. Settlement dana buyer.
		var settleID *uint64
		if len(plan.settleLines) > 0 {
			entry, err := txPosting.Create(ctx, ledger.CreateJournalRequest{
				TenantID: tenantID, Date: c.EventDate,
				Description: fmt.Sprintf("Pembatalan unit: penyelesaian dana buyer (cancellation #%d)", c.ID),
				Reference:   fmt.Sprintf("CX-%d-SETTLE", c.ID),
				Source:      "cancellation", CreatedBy: actorID,
				Lines: plan.settleLines,
			})
			if err != nil {
				return fmt.Errorf("jurnal settlement: %w", err)
			}
			if _, err := txPosting.Post(ctx, tenantID, entry.ID); err != nil {
				return fmt.Errorf("posting settlement: %w", err)
			}
			settleID = &entry.ID
		}

		// 6. Unit release + lifecycle log (pinned; Increment 6 pattern — jalur
		//    Cancellation resmi utk sold→available yang endpoint manual tolak).
		res := tx.Model(&project.Unit{}).
			Where("id = ? AND tenant_id = ? AND status = ?", c.UnitID, tenantID, string(plan.fromStatus)).
			Updates(map[string]interface{}{"status": string(project.UnitStatusAvailable),
				"buyer_ref": nil, "sale_date": nil, "sale_price": nil})
		if res.Error != nil {
			return fmt.Errorf("release unit: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return project.ErrUnitTransitionConflict
		}
		refID := c.ID
		if err := project.LogTransitionTx(tx, &project.UnitStatusTransition{
			TenantID: tenantID, UnitID: c.UnitID,
			FromStatus: plan.fromStatus, ToStatus: project.UnitStatusAvailable,
			Event: plan.unitEvent, EventDate: c.EventDate,
			ReferenceType: project.RefTypeCancellation, ReferenceID: &refID,
			ActorID: actorID, Notes: c.Reason,
		}); err != nil {
			return err
		}

		// 7. Kontrak: state → cancelled + supersede jadwal belum dibayar.
		if c.SaleContractID != nil {
			if err := tx.Table("sale_contracts").
				Where("id = ? AND tenant_id = ?", *c.SaleContractID, tenantID).
				Update("scheme_state", "cancelled").Error; err != nil {
				return fmt.Errorf("update kontrak: %w", err)
			}
			if err := tx.Table("payment_schedules").
				Where("tenant_id = ? AND sale_contract_id = ? AND status IN ?",
					tenantID, *c.SaleContractID, []string{"scheduled", "overdue"}).
				Update("status", "superseded").Error; err != nil {
				return fmt.Errorf("supersede jadwal: %w", err)
			}
		}

		// 8. Refund pending (bila ada sisa dana).
		var refundID *uint64
		if plan.refundNet.GreaterThan(domain.Zero) {
			cxID := c.ID
			rf := &Refund{
				TenantID: tenantID, SourceType: RefundFromCancellation,
				CancellationID: &cxID, UnitID: c.UnitID, Payee: plan.payee,
				Amount: plan.refundNet, Status: RefundPending, CreatedBy: actorID,
			}
			if err := tx.Create(rf).Error; err != nil {
				return fmt.Errorf("buat refund: %w", err)
			}
			refundID = &rf.ID
		}

		// 9. Dokumen → processed (pinned) + jejak jurnal + totals.
		now := time.Now()
		res = tx.Model(&Cancellation{}).
			Where("id = ? AND tenant_id = ? AND status = ?", c.ID, tenantID, string(StatusApproved)).
			Updates(map[string]interface{}{
				"status": string(StatusProcessed), "processed_at": &now, "processed_by": actorID,
				"received_total": plan.received.Decimal(), "refund_amount": plan.refundNet.Decimal(),
				"revenue_reversal_journal_id": revID, "cogs_reversal_journal_id": cogsID,
				"tax_reversal_journal_id": taxID, "settlement_journal_id": settleID,
				"refund_id": refundID, "active_key": nil,
				"land_sale_id": plan.landSaleID,
				"land_revenue_reversal_journal_id": landRevID, "land_cogs_reversal_journal_id": landCogsID,
			})
		if res.Error != nil {
			return fmt.Errorf("update processed: %w", res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: cancellation berubah bersamaan", ErrInvalidStateTransition)
		}
		c.Status, c.ProcessedAt, c.ProcessedBy = StatusProcessed, &now, actorID
		c.ReceivedTotal, c.RefundAmount = plan.received, plan.refundNet
		c.RevenueReversalJournalID, c.COGSReversalJournalID = revID, cogsID
		c.TaxReversalJournalID, c.SettlementJournalID, c.RefundID = taxID, settleID, refundID
		c.LandSaleID = plan.landSaleID
		c.LandRevenueReversalJournalID, c.LandCOGSReversalJournalID = landRevID, landCogsID
		out = &c
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ── Reads ─────────────────────────────────────────────────────────────────────

func (s *Service) Get(ctx context.Context, tenantID, id uint64) (*Cancellation, error) {
	var c Cancellation
	err := s.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).First(&c).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("baca cancellation: %w", err)
	}
	return &c, nil
}

func (s *Service) List(ctx context.Context, tenantID uint64, status Status) ([]*Cancellation, error) {
	q := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	var out []*Cancellation
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list cancellations: %w", err)
	}
	return out, nil
}
