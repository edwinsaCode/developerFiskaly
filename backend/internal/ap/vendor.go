package ap

import (
	"context"
	"strings"
)

// Master vendor. Tidak ada jurnal di file ini — vendor bukan peristiwa
// ekonomi, hanya identitas lawan transaksi.

// CreateVendorRequest adalah input pembuatan vendor.
type CreateVendorRequest struct {
	Name     string
	NPWP     string
	IsPKP    bool
	Address  string
	Phone    string
	Email    string
	BankName string
	BankAcc  string
	Note     string
}

// UpdateVendorRequest adalah perubahan atas vendor.
//
// Field pointer: nil = tidak diubah. Ini bukan kerapian belaka — dengan
// non-pointer, permintaan yang lupa menyertakan `is_pkp` akan diam-diam
// mengubah vendor PKP menjadi non-PKP, dan tagihan berikutnya menolak PPN yang
// sebenarnya sah.
type UpdateVendorRequest struct {
	Name     *string
	NPWP     *string
	IsPKP    *bool
	Address  *string
	Phone    *string
	Email    *string
	BankName *string
	BankAcc  *string
	Note     *string
	IsActive *bool
}

func (s *Service) CreateVendor(ctx context.Context, tenantID uint64, req CreateVendorRequest) (*Vendor, error) {
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, ErrVendorNameReq
	}
	v := &Vendor{
		TenantID: tenantID,
		Name:     name,
		NPWP:     strings.TrimSpace(req.NPWP),
		IsPKP:    req.IsPKP,
		Address:  req.Address,
		Phone:    req.Phone,
		Email:    req.Email,
		BankName: req.BankName,
		BankAcc:  req.BankAcc,
		IsActive: true,
		Note:     req.Note,
	}
	if err := s.repo.CreateVendor(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

// UpdateVendor mengubah master vendor.
//
// Menonaktifkan (IsActive=false) adalah satu-satunya bentuk "hapus" yang
// tersedia: menghapus barisnya akan memutus tautan pada tagihan dan baris biaya
// yang sudah terbit, sementara nominalnya tetap berdiri di buku besar.
func (s *Service) UpdateVendor(ctx context.Context, tenantID, id uint64, req UpdateVendorRequest) (*Vendor, error) {
	v, err := s.repo.FindVendor(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, ErrVendorNameReq
		}
		v.Name = name
	}
	setStr(&v.NPWP, req.NPWP)
	setStr(&v.Address, req.Address)
	setStr(&v.Phone, req.Phone)
	setStr(&v.Email, req.Email)
	setStr(&v.BankName, req.BankName)
	setStr(&v.BankAcc, req.BankAcc)
	setStr(&v.Note, req.Note)
	if req.IsPKP != nil {
		v.IsPKP = *req.IsPKP
	}
	if req.IsActive != nil {
		v.IsActive = *req.IsActive
	}
	if err := s.repo.SaveVendor(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}

func (s *Service) GetVendor(ctx context.Context, tenantID, id uint64) (*Vendor, error) {
	return s.repo.FindVendor(ctx, tenantID, id)
}

func (s *Service) ListVendors(ctx context.Context, tenantID uint64, f VendorFilter) ([]*Vendor, error) {
	return s.repo.ListVendors(ctx, tenantID, f)
}

func setStr(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}
