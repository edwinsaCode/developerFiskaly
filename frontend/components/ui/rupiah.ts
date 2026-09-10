// Aturan nilai rupiah + aturan kapan pesan error boleh terlihat.
//
// Dipisah dari `RupiahInput.tsx` supaya bisa diuji sebagai fungsi murni tanpa
// merender React. `RupiahInput.tsx` mengekspor ulang `validateRupiah`, jadi
// seluruh pemanggil lama (`import { validateRupiah } from "@/components/ui/RupiahInput"`)
// tidak berubah sama sekali.

// Validasi nilai rupiah: harus integer > 0 (default). `allowZero=true` — hanya
// dipakai Booking Fee (client final note 2026-09-10: fee 0 = tidak ada uang
// booking sama sekali, sah) — melonggarkan menjadi >= 0; pemanggil lain TIDAK
// berubah.
export function validateRupiah(raw: string, allowZero = false): string | null {
  if (!raw || raw === "" || (!allowZero && raw === "0")) return "Jumlah harus diisi";
  const n = parseInt(raw, 10);
  if (isNaN(n)) return "Jumlah tidak valid";
  if (allowZero ? n < 0 : n <= 0) {
    return allowZero ? "Jumlah tidak boleh negatif" : "Jumlah harus lebih dari Rp 0";
  }
  if (raw.includes(".")) return "Jumlah harus rupiah bulat (tanpa sen)";
  return null;
}

// shouldShowRupiahError menentukan apakah pesan error ditampilkan.
//
// Ada dua cara pemanggil menghitung `error`, dan keduanya sah:
//
//  1. EAGER — `error={validateRupiah(value) ?? undefined}` dihitung ulang tiap
//     render. Di sini kotak yang masih kosong BELUM salah, ia hanya belum
//     diisi; menampilkan "Jumlah harus diisi" merah saat modal baru terbuka
//     membuat form terlihat rusak sebelum disentuh. Karena itu error pada
//     nilai kosong ditelan — inilah perilaku bawaan, dan ia dipakai
//     DisbursementModal, BookingFormModal, dan ChargeGroupsPanel.
//
//  2. SAAT SUBMIT — `error={errors.amount}` hanya terisi setelah pengguna
//     menekan tombol simpan. Di sini menelan pesannya justru membuat tombol
//     terasa mati: form menolak submit tanpa memberi tahu alasannya (F-5).
//     Pemanggil seperti ini menyalakan `showErrorWhenEmpty`.
//
// Penanda "pernah di-blur" sengaja TIDAK dipakai: StrictMode menjalankan effect
// Modal dua kali di dev, dan cleanup-nya mengembalikan fokus sehingga field
// ter-blur sekali sebelum pengguna menyentuhnya.
export function shouldShowRupiahError(
  error: string | undefined,
  value: string,
  showErrorWhenEmpty?: boolean,
): boolean {
  if (!error) return false;
  if (value !== "") return true;
  return showErrorWhenEmpty === true;
}
