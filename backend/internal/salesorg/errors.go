package salesorg

import "errors"

var (
	ErrTeamNotFound   = errors.New("sales team tidak ditemukan")
	ErrTeamCodeDup    = errors.New("kode sales team sudah digunakan")
	ErrPersonNotFound = errors.New("sales person tidak ditemukan")
	ErrPersonCodeDup  = errors.New("kode sales person sudah digunakan")
	ErrCodeRequired   = errors.New("kode wajib diisi")
	ErrNameRequired   = errors.New("nama wajib diisi")
)
