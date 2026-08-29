package customer

import "errors"

var (
	ErrCustomerNotFound      = errors.New("customer tidak ditemukan")
	ErrCustomerCodeDuplicate = errors.New("kode customer sudah digunakan")
	ErrCustomerCodeRequired  = errors.New("kode customer wajib diisi")
	ErrCustomerNameRequired  = errors.New("nama customer wajib diisi")
	ErrCustomerTypeInvalid   = errors.New("tipe customer tidak valid: gunakan individual atau company")
)
