package notary

import "context"

// SeedLegacyDeposit membuka receiveDepositLegacy HANYA untuk test — file
// _test.go tidak ikut ter-compile ke biner produksi, jadi jalur masuk kedua
// tetap tertutup di runtime. Dipakai untuk menumbuhkan titipan warisan pra-W-1
// yang masih harus bisa dibayarkan dan direkonsiliasi.
func (s *Service) SeedLegacyDeposit(ctx context.Context, tenantID uint64, req ReceiveDepositRequest) (*NotaryDeposit, error) {
	return s.receiveDepositLegacy(ctx, tenantID, req)
}
