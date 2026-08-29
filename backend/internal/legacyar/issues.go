package legacyar

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONIssues adalah daftar keluhan atas satu baris berkas, disimpan di satu
// kolom JSON.
//
// Paket lain di repo ini menyimpan JSON sebagai `string` mentah dan membiarkan
// pemanggil melakukan Marshal/Unmarshal sendiri. Di sini tipenya sendiri yang
// mengurus konversi karena isinya IKUT KE LAYAR: pratinjau impor menampilkan
// pesan per kolom, dan kolom bertipe string akan sampai ke browser sebagai satu
// blok teks ber-escape yang harus di-parse ulang di sana.
type JSONIssues []RowIssue

// Value implements driver.Valuer.
func (i JSONIssues) Value() (driver.Value, error) {
	if len(i) == 0 {
		return nil, nil
	}
	b, err := json.Marshal([]RowIssue(i))
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// Scan implements sql.Scanner.
func (i *JSONIssues) Scan(src interface{}) error {
	if src == nil {
		*i = nil
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		return fmt.Errorf("legacyar: tipe tak didukung untuk issues: %T", src)
	}
	if len(b) == 0 {
		*i = nil
		return nil
	}
	var out []RowIssue
	if err := json.Unmarshal(b, &out); err != nil {
		return err
	}
	*i = out
	return nil
}

// add menambahkan keluhan baru.
func (i *JSONIssues) add(column, message string) {
	*i = append(*i, RowIssue{Column: column, Message: message})
}
