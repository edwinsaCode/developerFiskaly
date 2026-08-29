package billing

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
)

// GORMRepository implements ContractLoader, ScheduleLoader, and InvoiceStore.
type GORMRepository struct {
	db *gorm.DB
}

func NewGORMRepository(db *gorm.DB) *GORMRepository {
	return &GORMRepository{db: db}
}

// ── ContractLoader ────────────────────────────────────────────────────────────

func (r *GORMRepository) LoadContractInfo(ctx context.Context, tenantID, contractID uint64) (*ContractInfo, error) {
	type row struct {
		ID           uint64       `gorm:"column:id"`
		BuyerName    string       `gorm:"column:buyer_name"`
		BuyerID      string       `gorm:"column:buyer_id"`
		ContractDate time.Time    `gorm:"column:contract_date"`
		GrossAmount  domain.Money `gorm:"column:gross_amount"`
		DPPAmount    domain.Money `gorm:"column:dpp_amount"`
		IsPKP        bool         `gorm:"column:is_pkp"`
	}
	var res row
	err := r.db.WithContext(ctx).
		Table("sale_contracts").
		Select("id, buyer_name, buyer_id, contract_date, gross_amount, dpp_amount, is_pkp").
		Where("id = ? AND tenant_id = ?", contractID, tenantID).
		First(&res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrContractNotFound
		}
		return nil, fmt.Errorf("LoadContractInfo: %w", err)
	}
	return &ContractInfo{
		ID:           res.ID,
		BuyerName:    res.BuyerName,
		BuyerID:      res.BuyerID,
		ContractDate: res.ContractDate,
		GrossAmount:  res.GrossAmount,
		DPPAmount:    res.DPPAmount,
		IsPKP:        res.IsPKP,
	}, nil
}

// ── ScheduleLoader ────────────────────────────────────────────────────────────

func (r *GORMRepository) LoadScheduleInfo(ctx context.Context, tenantID, scheduleID uint64) (*ScheduleInfo, error) {
	type row struct {
		ID             uint64       `gorm:"column:id"`
		SaleContractID uint64       `gorm:"column:sale_contract_id"`
		UnitID         uint64       `gorm:"column:unit_id"`
		Amount         domain.Money `gorm:"column:amount"`
		DueDate        time.Time    `gorm:"column:due_date"`
		Type           string       `gorm:"column:type"`
		Status         string       `gorm:"column:status"`
	}
	var res row
	err := r.db.WithContext(ctx).
		Table("payment_schedules").
		Select("id, sale_contract_id, unit_id, amount, due_date, type, status").
		Where("id = ? AND tenant_id = ?", scheduleID, tenantID).
		First(&res).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrScheduleNotFound
		}
		return nil, fmt.Errorf("LoadScheduleInfo: %w", err)
	}
	return &ScheduleInfo{
		ID:             res.ID,
		SaleContractID: res.SaleContractID,
		UnitID:         res.UnitID,
		Amount:         res.Amount,
		DueDate:        res.DueDate,
		Type:           res.Type,
		Status:         res.Status,
	}, nil
}

// ── InvoiceStore ──────────────────────────────────────────────────────────────

func (r *GORMRepository) CreateInvoice(ctx context.Context, inv *Invoice) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return r.CreateInvoiceInTx(ctx, tx, inv)
	})
}

// CreateInvoiceInTx menerbitkan invoice DI DALAM transaksi pemanggil (W-5).
// Dibutuhkan karena sejak J-14a penerbitan invoice realisasi dan jurnal
// pengakuan piutangnya harus atomik: invoice tanpa jurnal berarti tagihan tanpa
// piutang, jurnal tanpa invoice berarti piutang tanpa dokumen (R-5 melarang
// keduanya).
func (r *GORMRepository) CreateInvoiceInTx(ctx context.Context, tx *gorm.DB, inv *Invoice) error {
	alloc, err := document.Allocate(tx.WithContext(ctx), inv.TenantID, document.TypeInvoice, inv.IssueDate)
	if err != nil {
		return err
	}
	inv.InvoiceNumber = alloc.Number
	if err := tx.WithContext(ctx).Create(inv).Error; err != nil {
		return err
	}
	var actor *uint64
	if inv.CreatedBy != 0 {
		actor = &inv.CreatedBy
	}
	_, err = document.Register(tx.WithContext(ctx), inv.TenantID, alloc,
		document.Source{Table: "invoices", ID: inv.ID}, inv.Amount, actor)
	return err
}

// ListByContractInTx membaca invoice kontrak lewat handle transaksi pemanggil.
func (r *GORMRepository) ListByContractInTx(ctx context.Context, tx *gorm.DB, tenantID, contractID uint64) ([]*Invoice, error) {
	var invs []*Invoice
	err := tx.WithContext(ctx).
		Where("tenant_id = ? AND sale_contract_id = ?", tenantID, contractID).
		Order("issue_date ASC, id ASC").
		Find(&invs).Error
	if err != nil {
		return nil, fmt.Errorf("ListByContractInTx: %w", err)
	}
	return invs, nil
}

func (r *GORMRepository) FindInvoiceByID(ctx context.Context, tenantID, id uint64) (*Invoice, error) {
	var inv Invoice
	err := r.db.WithContext(ctx).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		First(&inv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("FindInvoiceByID: %w", err)
	}
	return &inv, nil
}

func (r *GORMRepository) ListByContract(ctx context.Context, tenantID, contractID uint64) ([]*Invoice, error) {
	var invs []*Invoice
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND sale_contract_id = ?", tenantID, contractID).
		Order("issue_date ASC, id ASC").
		Find(&invs).Error
	if err != nil {
		return nil, fmt.Errorf("ListByContract: %w", err)
	}
	return invs, nil
}

func (r *GORMRepository) FindByScheduleID(ctx context.Context, tenantID, scheduleID uint64) (*Invoice, error) {
	var inv Invoice
	err := r.db.WithContext(ctx).
		Where("tenant_id = ? AND schedule_id = ?", tenantID, scheduleID).
		First(&inv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("FindByScheduleID: %w", err)
	}
	return &inv, nil
}

func (r *GORMRepository) UpdateStatus(ctx context.Context, tenantID, id uint64, status InvoiceStatus) error {
	res := r.db.WithContext(ctx).
		Model(&Invoice{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", string(status))
	if res.Error != nil {
		return fmt.Errorf("UpdateStatus: %w", res.Error)
	}
	return nil
}

// FindByScheduleIDInTx adalah varian tx-aware (sinkron invoice atomik, FE-2).
func (r *GORMRepository) FindByScheduleIDInTx(ctx context.Context, tx *gorm.DB, tenantID, scheduleID uint64) (*Invoice, error) {
	var inv Invoice
	err := tx.WithContext(ctx).
		Where("tenant_id = ? AND schedule_id = ?", tenantID, scheduleID).
		First(&inv).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInvoiceNotFound
		}
		return nil, fmt.Errorf("FindByScheduleIDInTx: %w", err)
	}
	return &inv, nil
}

// UpdateStatusInTx adalah varian tx-aware dari UpdateStatus.
func (r *GORMRepository) UpdateStatusInTx(ctx context.Context, tx *gorm.DB, tenantID, id uint64, status InvoiceStatus) error {
	res := tx.WithContext(ctx).
		Model(&Invoice{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Update("status", string(status))
	if res.Error != nil {
		return fmt.Errorf("UpdateStatusInTx: %w", res.Error)
	}
	return nil
}

// ── TenantInvoiceLister ───────────────────────────────────────────────────────

func (r *GORMRepository) ListAllByTenant(ctx context.Context, tenantID uint64) ([]*InvoiceSummary, error) {
	type row struct {
		ID             uint64        `gorm:"column:id"`
		SaleContractID uint64        `gorm:"column:sale_contract_id"`
		ScheduleID     *uint64       `gorm:"column:schedule_id"`
		InvoiceNumber  string        `gorm:"column:invoice_number"`
		InvoiceType    InvoiceType   `gorm:"column:invoice_type"`
		IssueDate      time.Time     `gorm:"column:issue_date"`
		DueDate        time.Time     `gorm:"column:due_date"`
		Amount         domain.Money  `gorm:"column:amount"`
		Status         InvoiceStatus `gorm:"column:status"`
		Notes          string        `gorm:"column:notes"`
		CreatedBy      uint64        `gorm:"column:created_by"`
		CreatedAt      time.Time     `gorm:"column:created_at"`
		UpdatedAt      time.Time     `gorm:"column:updated_at"`
		BuyerName      string        `gorm:"column:buyer_name"`
		UnitCode       string        `gorm:"column:unit_code"`
	}
	var rows []row
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			i.id, i.sale_contract_id, i.schedule_id, i.invoice_number, i.invoice_type,
			i.issue_date, i.due_date, i.amount, i.status, i.notes, i.created_by,
			i.created_at, i.updated_at,
			c.buyer_name,
			u.code AS unit_code
		FROM invoices i
		JOIN sale_contracts c ON c.id = i.sale_contract_id AND c.tenant_id = i.tenant_id
		JOIN units u ON u.id = c.unit_id AND u.tenant_id = i.tenant_id
		WHERE i.tenant_id = ?
		ORDER BY i.issue_date DESC, i.id DESC
	`, tenantID).Scan(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("ListAllByTenant: %w", err)
	}
	result := make([]*InvoiceSummary, len(rows))
	for i, rw := range rows {
		result[i] = &InvoiceSummary{
			Invoice: Invoice{
				ID:             rw.ID,
				TenantID:       tenantID,
				SaleContractID: rw.SaleContractID,
				ScheduleID:     rw.ScheduleID,
				InvoiceNumber:  rw.InvoiceNumber,
				InvoiceType:    rw.InvoiceType,
				IssueDate:      rw.IssueDate,
				DueDate:        rw.DueDate,
				Amount:         rw.Amount,
				Status:         rw.Status,
				Notes:          rw.Notes,
				CreatedBy:      rw.CreatedBy,
				CreatedAt:      rw.CreatedAt,
				UpdatedAt:      rw.UpdatedAt,
			},
			BuyerName: rw.BuyerName,
			UnitCode:  rw.UnitCode,
		}
	}
	return result, nil
}

// ── PrintLoader ───────────────────────────────────────────────────────────────

func (r *GORMRepository) LoadPrintData(ctx context.Context, tenantID, invoiceID uint64) (*PrintData, error) {
	type row struct {
		InvoiceNumber string        `gorm:"column:invoice_number"`
		InvoiceType   InvoiceType   `gorm:"column:invoice_type"`
		IssueDate     time.Time     `gorm:"column:issue_date"`
		DueDate       time.Time     `gorm:"column:due_date"`
		Amount        domain.Money  `gorm:"column:amount"`
		Status        InvoiceStatus `gorm:"column:status"`
		Notes         string        `gorm:"column:notes"`
		CompanyName   string        `gorm:"column:company_name"`
		BuyerName     string        `gorm:"column:buyer_name"`
		BuyerID       string        `gorm:"column:buyer_id"`
		PaymentType   string        `gorm:"column:payment_type"`
		ProjectName   string        `gorm:"column:project_name"`
		UnitCode      string        `gorm:"column:unit_code"`
		UnitType      string        `gorm:"column:unit_type"`
		UnitArea      string        `gorm:"column:unit_area"`
	}
	var res row
	err := r.db.WithContext(ctx).Raw(`
		SELECT
			i.invoice_number, i.invoice_type, i.issue_date, i.due_date, i.amount, i.status, i.notes,
			t.name        AS company_name,
			c.buyer_name, c.buyer_id, c.payment_type,
			p.name        AS project_name,
			u.code        AS unit_code, u.unit_type,
			CAST(u.saleable_area AS CHAR) AS unit_area
		FROM invoices i
		JOIN sale_contracts c ON c.id = i.sale_contract_id AND c.tenant_id = i.tenant_id
		JOIN units u ON u.id = c.unit_id AND u.tenant_id = i.tenant_id
		JOIN projects p ON p.id = u.project_id
		JOIN tenants t ON t.id = i.tenant_id
		WHERE i.id = ? AND i.tenant_id = ?
	`, invoiceID, tenantID).Scan(&res).Error
	if err != nil {
		return nil, fmt.Errorf("LoadPrintData: %w", err)
	}
	if res.InvoiceNumber == "" {
		return nil, ErrInvoiceNotFound
	}
	return &PrintData{
		InvoiceNumber: res.InvoiceNumber,
		InvoiceType:   res.InvoiceType,
		IssueDate:     res.IssueDate,
		DueDate:       res.DueDate,
		Amount:        res.Amount,
		Status:        res.Status,
		Notes:         res.Notes,
		CompanyName:   res.CompanyName,
		BuyerName:     res.BuyerName,
		BuyerID:       res.BuyerID,
		PaymentType:   res.PaymentType,
		ProjectName:   res.ProjectName,
		UnitCode:      res.UnitCode,
		UnitType:      res.UnitType,
		UnitArea:      res.UnitArea,
	}, nil
}

// ── Invoice number generation ─────────────────────────────────────────────────

// W-2: generator nomor invoice sendiri sudah DIHAPUS. Invoice memakai engine
// yang sama dengan seluruh dokumen lain (document.Allocate di CreateInvoice) —
// prefix INV dan format nomornya kini konfigurasi master, bukan literal di sini.


// AutoShortfallInvoiceEnabled membaca kebijakan tenant (kolom 000050).
func (r *GORMRepository) AutoShortfallInvoiceEnabled(ctx context.Context, tenantID uint64) (bool, error) {
	var enabled bool
	err := r.db.WithContext(ctx).Raw(
		`SELECT auto_shortfall_invoice FROM tenants WHERE id = ?`, tenantID,
	).Scan(&enabled).Error
	return enabled, err
}
