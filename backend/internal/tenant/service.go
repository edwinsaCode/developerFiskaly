package tenant

import (
	"context"
	"fmt"
	"strings"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/platform/auth"
)

// COASeeder menyemai Chart of Accounts default untuk tenant baru. Diimplementasikan
// oleh adapter ke ledger.SeedCOA di wiring layer (tenant tidak import ledger).
type COASeeder interface {
	SeedCOA(ctx context.Context, tenantID uint64) error
}

// ── Storage interfaces ────────────────────────────────────────────────────────

// TenantStore persists Tenant records.
type TenantStore interface {
	CreateTenant(ctx context.Context, t *Tenant) error
	FindTenantByID(ctx context.Context, id uint64) (*Tenant, error)
}

// UserStore persists User records.
type UserStore interface {
	CreateUser(ctx context.Context, u *User) error
	FindUserByEmail(ctx context.Context, email string) (*User, error)
	FindUserByID(ctx context.Context, tenantID, userID uint64) (*User, error)
	ListUsersByTenant(ctx context.Context, tenantID uint64) ([]*User, error)
	UpdateUser(ctx context.Context, u *User) error
}

// ── Request / Response types ──────────────────────────────────────────────────

// RegisterRequest is the input for creating a new tenant + owner account.
// Name bersifat opsional supaya klien lama (yang hanya mengirim tiga field)
// tetap berfungsi apa adanya.
type RegisterRequest struct {
	TenantName string `json:"tenant_name"`
	Email      string `json:"email"`
	Name       string `json:"name"`
	Password   string `json:"password"`
}

// RegisterResponse is returned after a successful registration.
type RegisterResponse struct {
	Token  string  `json:"token"`
	Tenant *Tenant `json:"tenant"`
	User   *User   `json:"user"`
}

// LoginRequest is the credential payload.
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse is returned after a successful login.
type LoginResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

// CreateUserRequest is used by an owner to add a new user to their tenant.
type CreateUserRequest struct {
	Email    string      `json:"email"`
	Name     string      `json:"name"`
	Password string      `json:"password"`
	Role     domain.Role `json:"role"`
}

// UpdateUserRequest mengubah user yang sudah ada. Semua field berupa pointer:
// nil berarti "jangan sentuh". Dengan begitu mengirim hanya {"name": "..."}
// tidak diam-diam mengosongkan role atau mereset password.
type UpdateUserRequest struct {
	Email    *string      `json:"email"`
	Name     *string      `json:"name"`
	Password *string      `json:"password"`
	Role     *domain.Role `json:"role"`
}

// MeResponse adalah identitas user yang sedang login — satu-satunya sumber
// yang boleh dipercaya frontend untuk menampilkan "siapa saya".
type MeResponse struct {
	ID          uint64      `json:"id"`
	Email       string      `json:"email"`
	Name        string      `json:"name"`
	DisplayName string      `json:"display_name"`
	Role        domain.Role `json:"role"`
	TenantID    uint64      `json:"tenant_id"`
	TenantName  string      `json:"tenant_name"`
}

// ── Service ───────────────────────────────────────────────────────────────────

// Service is the tenant/auth business-logic layer.
// It owns no DB access — only coordinates TenantStore, UserStore, and JWT.
type Service struct {
	tenants   TenantStore
	users     UserStore
	jwtSecret string
	jwtTTL    time.Duration
	// hashFn and checkFn are injectable so unit tests skip slow bcrypt.
	hashFn  func(password string) (string, error)
	checkFn func(hash, password string) error
	// coa menyemai COA default saat tenant baru dibuat (opsional; wajib di produksi).
	coa COASeeder
}

// SetCOASeeder memasang penyemai COA (dipanggil dari wiring layer).
func (s *Service) SetCOASeeder(c COASeeder) { s.coa = c }

// NewService constructs a Service using production bcrypt.
func NewService(tenants TenantStore, users UserStore, jwtSecret string, jwtTTL time.Duration) *Service {
	return &Service{
		tenants:   tenants,
		users:     users,
		jwtSecret: jwtSecret,
		jwtTTL:    jwtTTL,
		hashFn:    bcryptHash,
		checkFn:   bcryptCheck,
	}
}

// newTestService constructs a Service with fast stub hashing for unit tests.
// Exported with lowercase; only reachable from within the package (internal tests).
func newTestService(tenants TenantStore, users UserStore) *Service {
	return &Service{
		tenants:   tenants,
		users:     users,
		jwtSecret: "test-secret-32-chars-padded-xxxx",
		jwtTTL:    time.Hour,
		hashFn:    fakeHash,
		checkFn:   fakeCheck,
	}
}

// Register creates a new Tenant and its first Owner user, then returns a JWT.
// Returns ErrEmailAlreadyExists if the email is already taken.
// Returns ErrWeakPassword if the password is fewer than 8 characters.
func (s *Service) Register(ctx context.Context, req RegisterRequest) (*RegisterResponse, error) {
	if len(req.Password) < 8 {
		return nil, ErrWeakPassword
	}
	if req.TenantName == "" || req.Email == "" {
		return nil, ErrTenantNotFound
	}

	hash, err := s.hashFn(req.Password)
	if err != nil {
		return nil, err
	}

	t := &Tenant{Name: req.TenantName}
	if err := s.tenants.CreateTenant(ctx, t); err != nil {
		return nil, err
	}

	// Setiap tenant baru WAJIB punya Chart of Accounts agar bisa berakuntansi.
	if s.coa != nil {
		if err := s.coa.SeedCOA(ctx, t.ID); err != nil {
			return nil, fmt.Errorf("seed COA untuk tenant baru: %w", err)
		}
	}

	u := &User{
		TenantID:     t.ID,
		Email:        req.Email,
		Name:         strings.TrimSpace(req.Name),
		PasswordHash: hash,
		Role:         domain.RoleOwner,
	}
	if err := s.users.CreateUser(ctx, u); err != nil {
		return nil, err
	}

	token, err := auth.Generate(s.jwtSecret, t.ID, u.ID, string(u.Role), s.jwtTTL)
	if err != nil {
		return nil, err
	}
	return &RegisterResponse{Token: token, Tenant: t, User: u}, nil
}

// Login validates credentials and returns a JWT.
// Returns ErrInvalidCredentials for wrong email or wrong password.
func (s *Service) Login(ctx context.Context, req LoginRequest) (*LoginResponse, error) {
	u, err := s.users.FindUserByEmail(ctx, req.Email)
	if err != nil {
		return nil, ErrInvalidCredentials
	}
	if err := s.checkFn(u.PasswordHash, req.Password); err != nil {
		return nil, ErrInvalidCredentials
	}
	token, err := auth.Generate(s.jwtSecret, u.TenantID, u.ID, string(u.Role), s.jwtTTL)
	if err != nil {
		return nil, err
	}
	return &LoginResponse{Token: token, User: u}, nil
}

// CreateUser adds a new user to the tenant. Only the owner may do this.
func (s *Service) CreateUser(ctx context.Context, tenantID uint64, actorRole domain.Role, req CreateUserRequest) (*User, error) {
	if !actorRole.CanManageUsers() {
		return nil, ErrInsufficientRole
	}
	if len(req.Password) < 8 {
		return nil, ErrWeakPassword
	}
	if !req.Role.Valid() {
		return nil, ErrInvalidRole
	}
	hash, err := s.hashFn(req.Password)
	if err != nil {
		return nil, err
	}
	u := &User{
		TenantID:     tenantID,
		Email:        req.Email,
		Name:         strings.TrimSpace(req.Name),
		PasswordHash: hash,
		Role:         req.Role,
	}
	if err := s.users.CreateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// UpdateUser mengubah user dalam tenant yang sama. Hanya owner yang boleh.
//
// Dua penjaga penting:
//   - user dari tenant lain tidak pernah ditemukan (FindUserByID sudah
//     tenant-scoped, Invariant #6) sehingga tidak bisa disentuh;
//   - owner tidak boleh menurunkan role dirinya sendiri — kalau itu owner
//     terakhir, tenant kehilangan satu-satunya orang yang bisa mengelola user.
func (s *Service) UpdateUser(
	ctx context.Context,
	tenantID, actorUserID uint64,
	actorRole domain.Role,
	targetUserID uint64,
	req UpdateUserRequest,
) (*User, error) {
	if !actorRole.CanManageUsers() {
		return nil, ErrInsufficientRole
	}
	u, err := s.users.FindUserByID(ctx, tenantID, targetUserID)
	if err != nil {
		return nil, err
	}

	if req.Role != nil {
		if !req.Role.Valid() {
			return nil, ErrInvalidRole
		}
		if targetUserID == actorUserID && *req.Role != domain.RoleOwner {
			return nil, ErrCannotDemoteSelf
		}
		u.Role = *req.Role
	}
	if req.Name != nil {
		u.Name = strings.TrimSpace(*req.Name)
	}
	if req.Email != nil {
		email := strings.TrimSpace(*req.Email)
		if email == "" {
			return nil, ErrInvalidEmail
		}
		u.Email = email
	}
	if req.Password != nil {
		if len(*req.Password) < 8 {
			return nil, ErrWeakPassword
		}
		hash, err := s.hashFn(*req.Password)
		if err != nil {
			return nil, err
		}
		u.PasswordHash = hash
	}

	if err := s.users.UpdateUser(ctx, u); err != nil {
		return nil, err
	}
	return u, nil
}

// Me returns the identity of the currently authenticated user.
// Tenant name ikut dikembalikan supaya app shell tidak perlu request kedua.
func (s *Service) Me(ctx context.Context, tenantID, userID uint64) (*MeResponse, error) {
	u, err := s.users.FindUserByID(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	resp := &MeResponse{
		ID:          u.ID,
		Email:       u.Email,
		Name:        u.Name,
		DisplayName: u.DisplayName(),
		Role:        u.Role,
		TenantID:    u.TenantID,
	}
	// Nama tenant bersifat kosmetik: kalau gagal dibaca, identitas user tetap
	// dikembalikan. Header tanpa nama perusahaan jauh lebih baik daripada
	// app shell yang mengira sesi tidak valid.
	if t, err := s.tenants.FindTenantByID(ctx, tenantID); err == nil {
		resp.TenantName = t.Name
	}
	return resp, nil
}

// ListUsers returns all users for the given tenant.
func (s *Service) ListUsers(ctx context.Context, tenantID uint64) ([]*User, error) {
	return s.users.ListUsersByTenant(ctx, tenantID)
}
