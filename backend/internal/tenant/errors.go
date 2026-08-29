package tenant

import "errors"

var (
	ErrEmailAlreadyExists  = errors.New("email sudah terdaftar")
	ErrInvalidCredentials  = errors.New("email atau password salah")
	ErrTenantNotFound      = errors.New("tenant tidak ditemukan")
	ErrUserNotFound        = errors.New("user tidak ditemukan")
	ErrInsufficientRole    = errors.New("role tidak cukup untuk operasi ini")
	ErrWeakPassword        = errors.New("password minimal 8 karakter")
	ErrInvalidRole         = errors.New("role tidak valid: gunakan owner|accountant|marketing|viewer")

	// ErrCannotDemoteSelf melindungi tenant dari terkunci: bila owner terakhir
	// menurunkan role dirinya sendiri, tidak ada lagi yang bisa mengelola user.
	ErrCannotDemoteSelf = errors.New("owner tidak dapat menurunkan role dirinya sendiri")
	ErrInvalidEmail     = errors.New("email tidak boleh kosong")
)
