package document

import "errors"

var (
	// ErrTypeUnknown — kode jenis dokumen tidak terdaftar di master tenant.
	ErrTypeUnknown = errors.New("jenis dokumen tidak dikenal")
	// ErrTypeInactive — jenis dokumen ada tapi dinonaktifkan.
	ErrTypeInactive = errors.New("jenis dokumen nonaktif")
	// ErrTypeInvalid — konfigurasi master tidak sah (prefix/kebijakan/padding).
	ErrTypeInvalid = errors.New("konfigurasi jenis dokumen tidak sah")
	// ErrTypeDuplicate — kode jenis dokumen sudah dipakai tenant ini.
	ErrTypeDuplicate = errors.New("kode jenis dokumen sudah ada")
	// ErrTypeNotFound — id jenis dokumen tidak ditemukan.
	ErrTypeNotFound = errors.New("jenis dokumen tidak ditemukan")
	// ErrFormatInvalid — number_format tidak bisa dirender jadi nomor unik.
	ErrFormatInvalid = errors.New("format nomor dokumen tidak sah")
	// ErrSourceRequired — pendaftaran registry tanpa asal dokumen.
	ErrSourceRequired = errors.New("asal dokumen wajib diisi")
	// ErrNumberTaken — nomor sudah tercatat di registry (seharusnya mustahil;
	// muncul bila seri pernah dimundurkan manual).
	ErrNumberTaken = errors.New("nomor dokumen sudah terpakai")
	// ErrSourceHasDocument — baris sumber ini SUDAH punya dokumen (uk_doc_source).
	// Inilah larangan INV-DOC-1 "satu cash movement menghasilkan dua dokumen"
	// yang ditegakkan database, bukan gejala seri penomoran yang rusak.
	ErrSourceHasDocument = errors.New("sumber sudah memiliki dokumen")

	// ErrJournalNotLinkable — jurnal tidak bisa ditautkan ke dokumen: jurnalnya
	// tidak ada, milik tenant lain, atau SUDAH punya dokumen (W-3.1).
	// Kasus terakhir adalah larangan INV-DOC-1 "satu cash movement menghasilkan
	// dua document" — ditolak di sini, bukan diperbaiki belakangan.
	ErrJournalNotLinkable = errors.New("jurnal tidak bisa ditautkan ke dokumen")
	// ErrJournalRequired — penerbitan dokumen kas tanpa id jurnal.
	ErrJournalRequired = errors.New("id jurnal wajib diisi untuk dokumen kas")
)
