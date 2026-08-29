// Package crm mengaktifkan SEAM Lead/Prospect (blueprint §2 — CRM ringan).
// Master murni: TANPA ledger/jurnal. Funnel: new → contacted → qualified →
// converted (→ Customer master) | lost.
package crm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"esaproperti/internal/customer"
	"esaproperti/internal/platform/auth"
)

// ── Model ─────────────────────────────────────────────────────────────────────

type LeadStatus string

const (
	LeadNew       LeadStatus = "new"
	LeadContacted LeadStatus = "contacted"
	LeadQualified LeadStatus = "qualified"
	LeadConverted LeadStatus = "converted" // terminal
	LeadLost      LeadStatus = "lost"      // terminal
)

func (s LeadStatus) Valid() bool {
	switch s {
	case LeadNew, LeadContacted, LeadQualified, LeadConverted, LeadLost:
		return true
	}
	return false
}

// CanTransitionTo: maju bebas sepanjang funnel + lost dari non-terminal;
// converted HANYA via Convert (bukan set-status manual).
func (s LeadStatus) CanTransitionTo(next LeadStatus) bool {
	if s == LeadConverted || s == LeadLost {
		return false
	}
	switch next {
	case LeadContacted:
		return s == LeadNew
	case LeadQualified:
		return s == LeadNew || s == LeadContacted
	case LeadLost:
		return true
	}
	return false
}

type Lead struct {
	ID            uint64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID      uint64     `gorm:"not null;index"           json:"-"`
	Name          string     `gorm:"size:200;not null"        json:"name"`
	Phone         string     `gorm:"size:30"                  json:"phone,omitempty"`
	Email         string     `gorm:"size:120"                 json:"email,omitempty"`
	Source        string     `gorm:"size:30;default:'other'"  json:"source"`
	Status        LeadStatus `gorm:"size:20;default:'new'"    json:"status"`
	SalesPersonID *uint64    `json:"sales_person_id,omitempty"`
	ProjectID     *uint64    `json:"project_id,omitempty"`
	CustomerID    *uint64    `json:"customer_id,omitempty"`
	Notes         string     `gorm:"size:1000" json:"notes,omitempty"`
	LostReason    string     `gorm:"size:500"  json:"lost_reason,omitempty"`
	CreatedBy     *uint64    `json:"created_by,omitempty"`
	ConvertedAt   *time.Time `json:"converted_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (Lead) TableName() string { return "leads" }

var (
	ErrNotFound          = errors.New("lead tidak ditemukan")
	ErrInvalidTransition = errors.New("transisi status lead tidak valid")
	ErrNameRequired      = errors.New("nama lead wajib diisi")
	ErrAlreadyTerminal   = errors.New("lead sudah terminal (converted/lost)")
)

var validSources = map[string]bool{
	"walk_in": true, "referral": true, "online": true, "ads": true, "expo": true, "other": true,
}

// ── Service ───────────────────────────────────────────────────────────────────

type Service struct {
	db        *gorm.DB
	customers *customer.Service
}

func NewService(db *gorm.DB, customers *customer.Service) *Service {
	return &Service{db: db, customers: customers}
}

func (s *Service) Create(ctx context.Context, tenantID uint64, l *Lead) (*Lead, error) {
	if l.Name == "" {
		return nil, ErrNameRequired
	}
	if !validSources[l.Source] {
		l.Source = "other"
	}
	l.TenantID = tenantID
	l.Status = LeadNew
	if err := s.db.WithContext(ctx).Create(l).Error; err != nil {
		return nil, fmt.Errorf("simpan lead: %w", err)
	}
	return l, nil
}

type UpdateInput struct {
	Status        *LeadStatus
	SalesPersonID *uint64
	ProjectID     *uint64
	Notes         *string
	LostReason    *string
}

func (s *Service) Update(ctx context.Context, tenantID, id uint64, in UpdateInput) (*Lead, error) {
	l, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if in.Status != nil {
		if !in.Status.Valid() || *in.Status == LeadConverted {
			return nil, fmt.Errorf("%w: %s", ErrInvalidTransition, *in.Status)
		}
		if !l.Status.CanTransitionTo(*in.Status) {
			return nil, fmt.Errorf("%w: %s → %s", ErrInvalidTransition, l.Status, *in.Status)
		}
		updates["status"] = string(*in.Status)
		if *in.Status == LeadLost && in.LostReason != nil {
			updates["lost_reason"] = *in.LostReason
		}
	}
	if in.SalesPersonID != nil {
		updates["sales_person_id"] = *in.SalesPersonID
	}
	if in.ProjectID != nil {
		updates["project_id"] = *in.ProjectID
	}
	if in.Notes != nil {
		updates["notes"] = *in.Notes
	}
	if len(updates) == 0 {
		return l, nil
	}
	res := s.db.WithContext(ctx).Model(&Lead{}).
		Where("id = ? AND tenant_id = ?", id, tenantID).
		Updates(updates)
	if res.Error != nil {
		return nil, res.Error
	}
	return s.Get(ctx, tenantID, id)
}

// Convert: lead non-terminal → Customer master (atau tautkan ke customer
// existing) + status converted. Satu tx.
func (s *Service) Convert(ctx context.Context, tenantID, id uint64, existingCustomerID *uint64, actorID *uint64) (*Lead, *customer.Customer, error) {
	l, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, nil, err
	}
	if l.Status == LeadConverted || l.Status == LeadLost {
		return nil, nil, ErrAlreadyTerminal
	}

	var cust *customer.Customer
	if existingCustomerID != nil {
		cust, err = s.customers.Get(ctx, tenantID, *existingCustomerID)
		if err != nil {
			return nil, nil, err
		}
	} else {
		cust, err = s.customers.Create(ctx, tenantID, customer.CreateCustomerRequest{
			Code:      fmt.Sprintf("CUST-L%d-%d", id, time.Now().Unix()%100000),
			Name:      l.Name,
			Type:      customer.CustomerTypeIndividual,
			Phone:     l.Phone,
			Email:     l.Email,
			CreatedBy: actorID,
		})
		if err != nil {
			return nil, nil, fmt.Errorf("buat customer dari lead: %w", err)
		}
	}

	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Lead{}).
		Where("id = ? AND tenant_id = ? AND status NOT IN ('converted','lost')", id, tenantID).
		Updates(map[string]interface{}{
			"status": string(LeadConverted), "customer_id": cust.ID, "converted_at": &now,
		})
	if res.Error != nil {
		return nil, nil, res.Error
	}
	if res.RowsAffected == 0 {
		return nil, nil, ErrAlreadyTerminal
	}
	l.Status, l.CustomerID, l.ConvertedAt = LeadConverted, &cust.ID, &now
	return l, cust, nil
}

func (s *Service) Get(ctx context.Context, tenantID, id uint64) (*Lead, error) {
	var l Lead
	err := s.db.WithContext(ctx).Where("id = ? AND tenant_id = ?", id, tenantID).First(&l).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &l, nil
}

func (s *Service) List(ctx context.Context, tenantID uint64, status LeadStatus, salesPersonID uint64) ([]*Lead, error) {
	q := s.db.WithContext(ctx).Where("tenant_id = ?", tenantID)
	if status != "" {
		q = q.Where("status = ?", string(status))
	}
	if salesPersonID != 0 {
		q = q.Where("sales_person_id = ?", salesPersonID)
	}
	var out []*Lead
	if err := q.Order("id DESC").Find(&out).Error; err != nil {
		return nil, err
	}
	return out, nil
}

// ── Handler ───────────────────────────────────────────────────────────────────

type Handler struct{ svc *Service }

func NewHandler(db *gorm.DB) *Handler {
	custSvc := customer.NewService(customer.NewGORMRepository(db))
	return &Handler{svc: NewService(db, custSvc)}
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/leads", func(r chi.Router) {
		r.Get("/", h.list)
		r.With(auth.RequireSalesWrite()).Post("/", h.create)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", h.get)
			r.With(auth.RequireSalesWrite()).Put("/", h.update)
			r.With(auth.RequireSalesWrite()).Post("/convert", h.convert)
		})
	})
}

func crmJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func crmErr(w http.ResponseWriter, status int, msg string) {
	crmJSON(w, status, map[string]string{"error": msg})
}

func crmAuth(r *http.Request) (uint64, *uint64, bool) {
	tid, ok := auth.TenantIDFrom(r.Context())
	if !ok {
		return 0, nil, false
	}
	var actor *uint64
	if uid, uok := auth.UserIDFrom(r.Context()); uok {
		actor = &uid
	}
	return tid, actor, true
}

func mapCrmErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound), errors.Is(err, customer.ErrCustomerNotFound):
		crmErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrInvalidTransition), errors.Is(err, ErrAlreadyTerminal):
		crmErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrNameRequired):
		crmErr(w, http.StatusBadRequest, err.Error())
	default:
		crmErr(w, http.StatusInternalServerError, err.Error())
	}
}

type leadDTO struct {
	Name          string  `json:"name"`
	Phone         string  `json:"phone,omitempty"`
	Email         string  `json:"email,omitempty"`
	Source        string  `json:"source,omitempty"`
	SalesPersonID *uint64 `json:"sales_person_id,omitempty"`
	ProjectID     *uint64 `json:"project_id,omitempty"`
	Notes         string  `json:"notes,omitempty"`
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := crmAuth(r)
	if !ok {
		crmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	var dto leadDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		crmErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	l := &Lead{
		Name: dto.Name, Phone: dto.Phone, Email: dto.Email, Source: dto.Source,
		SalesPersonID: dto.SalesPersonID, ProjectID: dto.ProjectID,
		Notes: dto.Notes, CreatedBy: actor,
	}
	created, err := h.svc.Create(r.Context(), tid, l)
	if err != nil {
		mapCrmErr(w, err)
		return
	}
	crmJSON(w, http.StatusCreated, created)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := crmAuth(r)
	if !ok {
		crmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	var spID uint64
	if q := r.URL.Query().Get("sales_person_id"); q != "" {
		spID, _ = strconv.ParseUint(q, 10, 64)
	}
	ls, err := h.svc.List(r.Context(), tid, LeadStatus(r.URL.Query().Get("status")), spID)
	if err != nil {
		mapCrmErr(w, err)
		return
	}
	if ls == nil {
		ls = []*Lead{}
	}
	crmJSON(w, http.StatusOK, ls)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := crmAuth(r)
	if !ok {
		crmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		crmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	l, err := h.svc.Get(r.Context(), tid, id)
	if err != nil {
		mapCrmErr(w, err)
		return
	}
	crmJSON(w, http.StatusOK, l)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	tid, _, ok := crmAuth(r)
	if !ok {
		crmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		crmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		Status        *string `json:"status,omitempty"`
		SalesPersonID *uint64 `json:"sales_person_id,omitempty"`
		ProjectID     *uint64 `json:"project_id,omitempty"`
		Notes         *string `json:"notes,omitempty"`
		LostReason    *string `json:"lost_reason,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		crmErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	in := UpdateInput{
		SalesPersonID: dto.SalesPersonID, ProjectID: dto.ProjectID,
		Notes: dto.Notes, LostReason: dto.LostReason,
	}
	if dto.Status != nil {
		st := LeadStatus(*dto.Status)
		in.Status = &st
	}
	l, err := h.svc.Update(r.Context(), tid, id, in)
	if err != nil {
		mapCrmErr(w, err)
		return
	}
	crmJSON(w, http.StatusOK, l)
}

func (h *Handler) convert(w http.ResponseWriter, r *http.Request) {
	tid, actor, ok := crmAuth(r)
	if !ok {
		crmErr(w, http.StatusUnauthorized, "missing authentication context")
		return
	}
	id, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		crmErr(w, http.StatusBadRequest, "id tidak valid")
		return
	}
	var dto struct {
		CustomerID *uint64 `json:"customer_id,omitempty"` // tautkan ke existing; kosong = buat baru
	}
	_ = json.NewDecoder(r.Body).Decode(&dto)
	l, cust, err := h.svc.Convert(r.Context(), tid, id, dto.CustomerID, actor)
	if err != nil {
		mapCrmErr(w, err)
		return
	}
	crmJSON(w, http.StatusOK, map[string]any{"lead": l, "customer": cust})
}
