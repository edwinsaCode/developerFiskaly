package ap

import (
	"context"

	"gorm.io/gorm"

	"esaproperti/internal/cost"
)

// CostLinePlanner adalah SATU-SATUNYA sumber aturan "baris biaya yang sah"
// (A-1). Implementasi produksinya `*cost.Service` lewat `PlanAPLine`.
//
// Ia seam, bukan karena paket ini ingin fleksibel, melainkan supaya kompilator
// ikut menjaga janjinya: satu-satunya cara `ap` menentukan akun debit, tier,
// dan kelayakan sebuah baris adalah lewat pintu ini. Menambahkan validasi
// biaya di dalam `ap` berarti menulis aturan kedua yang akan menyimpang
// diam-diam dari jalur biaya kas — dan yang menyimpang adalah angka RAB.
type CostLinePlanner interface {
	PlanAPLine(ctx context.Context, tenantID uint64, req cost.CreateCostEntryRequest) (cost.CostLinePlan, error)
}

// ApprovalGate menegakkan governance dokumen (INV-AP-19, D-20).
//
// OPT-IN: tenant yang belum mengkonfigurasi workflow tidak terhalang apa pun —
// perilakunya persis seperti sebelum modul ini ada. nil = tanpa gate sama
// sekali (unit test).
type ApprovalGate interface {
	RequireApproved(ctx context.Context, tenantID, invoiceID uint64) error
}

// Service adalah pintu masuk domain hutang usaha.
//
// Ia memegang `*gorm.DB` dan membuka transaksinya sendiri — pola kanonik repo
// ini (`legacyar.Service`, `charge.Service`). Kolaborator ledger dibangun DI
// DALAM transaksi (`ledger.NewPostingService(txLedger, txLedger)`), sehingga
// jurnal, baris biaya, dan baris tagihan mustahil terpisah oleh kegagalan di
// tengah jalan.
type Service struct {
	db      *gorm.DB
	repo    *Repository
	planner CostLinePlanner
	gate    ApprovalGate
}

// NewService membangun service dengan planner biaya yang WAJIB ada.
//
// Sengaja parameter, bukan setter opsional: tanpa planner tidak ada satu pun
// baris biaya yang bisa divalidasi, dan service yang bisa dibangun setengah
// jadi hanya akan gagal jauh dari tempat kesalahannya dibuat.
func NewService(db *gorm.DB, planner CostLinePlanner) *Service {
	return &Service{db: db, repo: NewRepository(db), planner: planner}
}

// WithApprovalGate memasang gate approval (fluent).
func (s *Service) WithApprovalGate(g ApprovalGate) *Service {
	s.gate = g
	return s
}

// Repo membuka repository untuk pembacaan (dipakai handler & test).
func (s *Service) Repo() *Repository { return s.repo }
