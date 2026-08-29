package tenant

// Internal test (package tenant, not tenant_test) so we can use newTestService
// and access the unexported fakeHash/fakeCheck helpers.

import (
	"context"
	"errors"
	"sync"
	"testing"

	"esaproperti/internal/domain"
)

// ── In-memory mocks ───────────────────────────────────────────────────────────

type mockTenantStore struct {
	mu     sync.Mutex
	items  map[uint64]*Tenant
	nextID uint64
}

func newMockTenantStore() *mockTenantStore {
	return &mockTenantStore{items: make(map[uint64]*Tenant)}
}

func (m *mockTenantStore) CreateTenant(_ context.Context, t *Tenant) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	t.ID = m.nextID
	cp := *t
	m.items[t.ID] = &cp
	return nil
}

func (m *mockTenantStore) FindTenantByID(_ context.Context, id uint64) (*Tenant, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	t, ok := m.items[id]
	if !ok {
		return nil, ErrTenantNotFound
	}
	cp := *t
	return &cp, nil
}

type mockUserStore struct {
	mu     sync.Mutex
	byID   map[uint64]*User
	byEmail map[string]*User
	nextID uint64
}

func newMockUserStore() *mockUserStore {
	return &mockUserStore{
		byID:    make(map[uint64]*User),
		byEmail: make(map[string]*User),
	}
}

func (m *mockUserStore) CreateUser(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.byEmail[u.Email]; exists {
		return ErrEmailAlreadyExists
	}
	m.nextID++
	u.ID = m.nextID
	cp := *u
	m.byID[u.ID] = &cp
	m.byEmail[u.Email] = &cp
	return nil
}

func (m *mockUserStore) FindUserByEmail(_ context.Context, email string) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byEmail[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *mockUserStore) FindUserByID(_ context.Context, tenantID, userID uint64) (*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, ok := m.byID[userID]
	if !ok || u.TenantID != tenantID {
		return nil, ErrUserNotFound
	}
	cp := *u
	return &cp, nil
}

func (m *mockUserStore) UpdateUser(_ context.Context, u *User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	existing, ok := m.byID[u.ID]
	if !ok || existing.TenantID != u.TenantID {
		return ErrUserNotFound
	}
	if other, taken := m.byEmail[u.Email]; taken && other.ID != u.ID {
		return ErrEmailAlreadyExists
	}
	delete(m.byEmail, existing.Email)
	cp := *u
	m.byID[u.ID] = &cp
	m.byEmail[u.Email] = &cp
	return nil
}

func (m *mockUserStore) ListUsersByTenant(_ context.Context, tenantID uint64) ([]*User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*User
	for _, u := range m.byID {
		if u.TenantID == tenantID {
			cp := *u
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ── Helper ────────────────────────────────────────────────────────────────────

func newSvc() (*Service, *mockTenantStore, *mockUserStore) {
	ts := newMockTenantStore()
	us := newMockUserStore()
	return newTestService(ts, us), ts, us
}

func registerOwner(t *testing.T, svc *Service) *RegisterResponse {
	t.Helper()
	resp, err := svc.Register(context.Background(), RegisterRequest{
		TenantName: "PT Maju Jaya",
		Email:      "owner@example.com",
		Password:   "password123",
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	return resp
}

// ── Registration tests ────────────────────────────────────────────────────────

func TestRegister_Success_ReturnsTokenAndOwnerUser(t *testing.T) {
	svc, _, _ := newSvc()
	resp := registerOwner(t, svc)

	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.Tenant == nil || resp.Tenant.ID == 0 {
		t.Error("expected tenant with ID")
	}
	if resp.User == nil || resp.User.ID == 0 {
		t.Error("expected user with ID")
	}
	if resp.User.Role != domain.RoleOwner {
		t.Errorf("expected role owner, got %s", resp.User.Role)
	}
	if resp.User.PasswordHash != "" && resp.User.PasswordHash == "password123" {
		t.Error("password must be hashed, not stored in plain text")
	}
}

func TestRegister_TenantAndUserShareTenantID(t *testing.T) {
	svc, _, _ := newSvc()
	resp := registerOwner(t, svc)
	if resp.User.TenantID != resp.Tenant.ID {
		t.Errorf("user.TenantID %d != tenant.ID %d", resp.User.TenantID, resp.Tenant.ID)
	}
}

func TestRegister_DuplicateEmail_ReturnsError(t *testing.T) {
	svc, _, _ := newSvc()
	registerOwner(t, svc)

	_, err := svc.Register(context.Background(), RegisterRequest{
		TenantName: "Proyek Lain",
		Email:      "owner@example.com", // same email
		Password:   "differentpass",
	})
	if err == nil {
		t.Fatal("expected error for duplicate email, got nil")
	}
}

func TestRegister_WeakPassword_ReturnsErrWeakPassword(t *testing.T) {
	svc, _, _ := newSvc()
	_, err := svc.Register(context.Background(), RegisterRequest{
		TenantName: "PT ABC",
		Email:      "new@example.com",
		Password:   "short", // < 8 chars
	})
	if !errors.Is(err, ErrWeakPassword) {
		t.Errorf("expected ErrWeakPassword, got %v", err)
	}
}

func TestRegister_EmptyTenantName_ReturnsError(t *testing.T) {
	svc, _, _ := newSvc()
	_, err := svc.Register(context.Background(), RegisterRequest{
		TenantName: "",
		Email:      "x@example.com",
		Password:   "validpass",
	})
	if err == nil {
		t.Error("expected error for empty tenant name")
	}
}

// ── Login tests ───────────────────────────────────────────────────────────────

func TestLogin_ValidCredentials_ReturnsToken(t *testing.T) {
	svc, _, _ := newSvc()
	registerOwner(t, svc)

	resp, err := svc.Login(context.Background(), LoginRequest{
		Email:    "owner@example.com",
		Password: "password123",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if resp.Token == "" {
		t.Error("expected non-empty token")
	}
	if resp.User == nil {
		t.Error("expected non-nil user in response")
	}
}

func TestLogin_WrongPassword_ReturnsErrInvalidCredentials(t *testing.T) {
	svc, _, _ := newSvc()
	registerOwner(t, svc)

	_, err := svc.Login(context.Background(), LoginRequest{
		Email:    "owner@example.com",
		Password: "wrongpassword",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestLogin_UnknownEmail_ReturnsErrInvalidCredentials(t *testing.T) {
	svc, _, _ := newSvc()

	_, err := svc.Login(context.Background(), LoginRequest{
		Email:    "nobody@example.com",
		Password: "somepassword",
	})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

// TestLogin_DoesNotLeakWhichFieldIsWrong: both unknown email and wrong password
// must return the same error, preventing user enumeration.
func TestLogin_DoesNotLeakWhetherEmailExists(t *testing.T) {
	svc, _, _ := newSvc()
	registerOwner(t, svc)

	_, err1 := svc.Login(context.Background(), LoginRequest{Email: "nobody@x.com", Password: "pass"})
	_, err2 := svc.Login(context.Background(), LoginRequest{Email: "owner@example.com", Password: "wrong"})

	if !errors.Is(err1, ErrInvalidCredentials) || !errors.Is(err2, ErrInvalidCredentials) {
		t.Errorf("both errors must be ErrInvalidCredentials: err1=%v err2=%v", err1, err2)
	}
}

// ── CreateUser tests ──────────────────────────────────────────────────────────

func TestCreateUser_OwnerCanAddAccountant(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	user, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email:    "accountant@example.com",
		Password: "password99",
		Role:     domain.RoleAccountant,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if user.TenantID != reg.Tenant.ID {
		t.Errorf("new user must belong to the same tenant")
	}
	if user.Role != domain.RoleAccountant {
		t.Errorf("expected role accountant, got %s", user.Role)
	}
}

func TestCreateUser_ViewerCannotAddUsers(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	_, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleViewer, CreateUserRequest{
		Email:    "hacker@example.com",
		Password: "password99",
		Role:     domain.RoleViewer,
	})
	if !errors.Is(err, ErrInsufficientRole) {
		t.Errorf("expected ErrInsufficientRole, got %v", err)
	}
}

func TestCreateUser_AccountantCannotAddUsers(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	_, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleAccountant, CreateUserRequest{
		Email:    "try@example.com",
		Password: "password99",
		Role:     domain.RoleViewer,
	})
	if !errors.Is(err, ErrInsufficientRole) {
		t.Errorf("expected ErrInsufficientRole, got %v", err)
	}
}

func TestCreateUser_WeakPassword_ReturnsError(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	_, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email:    "new@example.com",
		Password: "abc",
		Role:     domain.RoleViewer,
	})
	if !errors.Is(err, ErrWeakPassword) {
		t.Errorf("expected ErrWeakPassword, got %v", err)
	}
}

func TestCreateUser_InvalidRole_ReturnsError(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	_, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email:    "new2@example.com",
		Password: "validpass",
		Role:     domain.Role("superadmin"), // not a valid role
	})
	if !errors.Is(err, ErrInvalidRole) {
		t.Errorf("expected ErrInvalidRole, got %v", err)
	}
}

// ── ListUsers tests ───────────────────────────────────────────────────────────

func TestListUsers_ReturnsOnlyThisTenantUsers(t *testing.T) {
	svc, _, _ := newSvc()

	// Register two separate tenants.
	r1, _ := svc.Register(context.Background(), RegisterRequest{TenantName: "A", Email: "a@a.com", Password: "password1"})
	r2, _ := svc.Register(context.Background(), RegisterRequest{TenantName: "B", Email: "b@b.com", Password: "password2"})

	usersA, err := svc.ListUsers(context.Background(), r1.Tenant.ID)
	if err != nil {
		t.Fatalf("ListUsers: %v", err)
	}
	if len(usersA) != 1 {
		t.Errorf("expected 1 user for tenant A, got %d", len(usersA))
	}
	if usersA[0].TenantID != r1.Tenant.ID {
		t.Errorf("user belongs to wrong tenant")
	}

	usersB, _ := svc.ListUsers(context.Background(), r2.Tenant.ID)
	if len(usersB) != 1 || usersB[0].TenantID != r2.Tenant.ID {
		t.Errorf("tenant B should only see its own users")
	}
}

// ── W-12: identitas user (nama, edit, /auth/me) ───────────────────────────────

func TestUser_DisplayNameFallsBackToEmail(t *testing.T) {
	// User yang dibuat sebelum W-12 tidak punya nama. Layar harus menampilkan
	// email — bukan nama karangan, bukan string kosong.
	u := User{Email: "owner@example.com"}
	if got := u.DisplayName(); got != "owner@example.com" {
		t.Errorf("nama kosong: mau email, dapat %q", got)
	}
	u.Name = "Budi Santoso"
	if got := u.DisplayName(); got != "Budi Santoso" {
		t.Errorf("nama terisi: mau nama, dapat %q", got)
	}
}

func TestRegister_TanpaNamaTetapBerhasil(t *testing.T) {
	// Klien lama hanya mengirim tenant_name/email/password.
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)
	if reg.User.Name != "" {
		t.Errorf("nama harus kosong, bukan dikarang: %q", reg.User.Name)
	}
	if reg.User.DisplayName() != reg.User.Email {
		t.Errorf("fallback tampilan harus email, dapat %q", reg.User.DisplayName())
	}
}

func TestCreateUser_MenyimpanNama(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	u, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email:    "sales@example.com",
		Name:     "  Siti Marketing  ",
		Password: "password99",
		Role:     domain.RoleMarketing,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}
	if u.Name != "Siti Marketing" {
		t.Errorf("nama harus di-trim: %q", u.Name)
	}
	if u.Role != domain.RoleMarketing {
		t.Errorf("role marketing tidak tersimpan: %s", u.Role)
	}
}

func TestCreateUser_MarketingAdalahRoleValid(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	if _, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email: "m@example.com", Password: "password99", Role: domain.RoleMarketing,
	}); err != nil {
		t.Fatalf("marketing harus diterima sebagai role: %v", err)
	}
}

func TestUpdateUser_OwnerUbahNamaDanRole(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)
	target, err := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email: "staf@example.com", Password: "password99", Role: domain.RoleViewer,
	})
	if err != nil {
		t.Fatalf("CreateUser: %v", err)
	}

	name := "Ahmad Staf"
	role := domain.RoleMarketing
	got, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, reg.User.ID, domain.RoleOwner,
		target.ID, UpdateUserRequest{Name: &name, Role: &role})
	if err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if got.Name != "Ahmad Staf" || got.Role != domain.RoleMarketing {
		t.Errorf("perubahan tidak tersimpan: %+v", got)
	}
	// Field yang tidak dikirim tidak boleh berubah.
	if got.Email != "staf@example.com" {
		t.Errorf("email ikut berubah padahal tidak dikirim: %q", got.Email)
	}
	if got.PasswordHash == "" {
		t.Errorf("password hash terhapus oleh PATCH parsial")
	}
}

func TestUpdateUser_PasswordBaruBisaDipakaiLogin(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)
	target, _ := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email: "ganti@example.com", Password: "passwordlama", Role: domain.RoleViewer,
	})

	baru := "passwordbaru"
	if _, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, reg.User.ID, domain.RoleOwner,
		target.ID, UpdateUserRequest{Password: &baru}); err != nil {
		t.Fatalf("UpdateUser: %v", err)
	}
	if _, err := svc.Login(context.Background(), LoginRequest{Email: "ganti@example.com", Password: "passwordbaru"}); err != nil {
		t.Errorf("login dengan password baru gagal: %v", err)
	}
	if _, err := svc.Login(context.Background(), LoginRequest{Email: "ganti@example.com", Password: "passwordlama"}); err == nil {
		t.Errorf("password lama masih diterima setelah diganti")
	}
}

func TestUpdateUser_PasswordLemahDitolak(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)
	target, _ := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email: "lemah@example.com", Password: "password99", Role: domain.RoleViewer,
	})

	pendek := "abc"
	_, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, reg.User.ID, domain.RoleOwner,
		target.ID, UpdateUserRequest{Password: &pendek})
	if !errors.Is(err, ErrWeakPassword) {
		t.Errorf("mau ErrWeakPassword, dapat %v", err)
	}
}

func TestUpdateUser_NonOwnerDitolak(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)
	target, _ := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email: "korban@example.com", Password: "password99", Role: domain.RoleViewer,
	})

	naik := domain.RoleOwner
	for _, actor := range []domain.Role{domain.RoleAccountant, domain.RoleMarketing, domain.RoleViewer} {
		_, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, target.ID, actor,
			target.ID, UpdateUserRequest{Role: &naik})
		if !errors.Is(err, ErrInsufficientRole) {
			t.Errorf("actor %s: mau ErrInsufficientRole, dapat %v", actor, err)
		}
	}
}

func TestUpdateUser_OwnerTidakBisaMenurunkanDiriSendiri(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	turun := domain.RoleViewer
	_, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, reg.User.ID, domain.RoleOwner,
		reg.User.ID, UpdateUserRequest{Role: &turun})
	if !errors.Is(err, ErrCannotDemoteSelf) {
		t.Errorf("mau ErrCannotDemoteSelf, dapat %v", err)
	}

	// Tapi mengubah namanya sendiri tetap boleh.
	nama := "Owner Baru"
	if _, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, reg.User.ID, domain.RoleOwner,
		reg.User.ID, UpdateUserRequest{Name: &nama}); err != nil {
		t.Errorf("owner ubah nama sendiri harus boleh: %v", err)
	}
}

func TestUpdateUser_RoleTidakValidDitolak(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)
	target, _ := svc.CreateUser(context.Background(), reg.Tenant.ID, domain.RoleOwner, CreateUserRequest{
		Email: "x@example.com", Password: "password99", Role: domain.RoleViewer,
	})

	palsu := domain.Role("superadmin")
	_, err := svc.UpdateUser(context.Background(), reg.Tenant.ID, reg.User.ID, domain.RoleOwner,
		target.ID, UpdateUserRequest{Role: &palsu})
	if !errors.Is(err, ErrInvalidRole) {
		t.Errorf("mau ErrInvalidRole, dapat %v", err)
	}
}

// Invariant #6: user tenant lain tidak boleh ditemukan, apalagi diubah.
func TestUpdateUser_TidakBisaMenyentuhTenantLain(t *testing.T) {
	svc, _, _ := newSvc()
	a := registerOwner(t, svc)
	b, err := svc.Register(context.Background(), RegisterRequest{
		TenantName: "PT Sebelah", Email: "sebelah@example.com", Password: "password123",
	})
	if err != nil {
		t.Fatalf("Register tenant B: %v", err)
	}

	role := domain.RoleViewer
	_, err = svc.UpdateUser(context.Background(), a.Tenant.ID, a.User.ID, domain.RoleOwner,
		b.User.ID, UpdateUserRequest{Role: &role})
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("mau ErrUserNotFound untuk user lintas tenant, dapat %v", err)
	}
	// Dan korban tidak berubah.
	after, _ := svc.Me(context.Background(), b.Tenant.ID, b.User.ID)
	if after.Role != domain.RoleOwner {
		t.Errorf("role user tenant lain berubah menjadi %s", after.Role)
	}
}

func TestMe_MengembalikanIdentitasLengkap(t *testing.T) {
	svc, _, _ := newSvc()
	reg := registerOwner(t, svc)

	me, err := svc.Me(context.Background(), reg.Tenant.ID, reg.User.ID)
	if err != nil {
		t.Fatalf("Me: %v", err)
	}
	if me.ID != reg.User.ID || me.Email != "owner@example.com" || me.Role != domain.RoleOwner {
		t.Errorf("identitas tidak cocok: %+v", me)
	}
	if me.TenantID != reg.Tenant.ID || me.TenantName != "PT Maju Jaya" {
		t.Errorf("tenant tidak cocok: %+v", me)
	}
	if me.DisplayName != "owner@example.com" {
		t.Errorf("display_name tanpa nama harus email, dapat %q", me.DisplayName)
	}
}

func TestMe_TidakBisaDibacaLintasTenant(t *testing.T) {
	svc, _, _ := newSvc()
	a := registerOwner(t, svc)
	b, _ := svc.Register(context.Background(), RegisterRequest{
		TenantName: "PT Sebelah", Email: "sebelah2@example.com", Password: "password123",
	})

	if _, err := svc.Me(context.Background(), a.Tenant.ID, b.User.ID); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("mau ErrUserNotFound, dapat %v", err)
	}
}
