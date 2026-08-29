// <Rupiah> — format angka rupiah dari API.
// INVARIANT: frontend tidak menghitung uang — hanya memformat untuk tampilan.
// Format output: "Rp 2.800.000.000" (titik ribuan Indonesia, rata kanan, tabular).

type RupiahProps = {
  /** String decimal dari API ("2800000000"), number, null, atau undefined. */
  value?: string | number | null;
  /** Tambahkan kelas tambahan (misal untuk alignment). */
  className?: string;
  /** Tampilkan tanda minus merah untuk nilai negatif. Default true. */
  colorSign?: boolean;
};

// toNumeric — konversi aman ke number. Hanya untuk DISPLAY; tidak pernah
// dipakai sebagai basis perhitungan uang (itu tugas backend).
function toNumeric(value?: string | number | null): number {
  if (value == null) return 0;
  if (typeof value === "number") return value;
  // String dari API: "2800000000" atau "2800000000.0000"
  // Ganti koma desimal (jika ada) ke titik agar parseFloat bekerja.
  const numeric = parseFloat(String(value).replace(",", "."));
  return isNaN(numeric) ? 0 : numeric;
}

// formatRupiah — versi string dari <Rupiah>, untuk tempat yang hanya menerima
// teks (toast, title/tooltip, aria-label). Dipakai agar nominal di toast tidak
// tampil sebagai angka mentah "20000000".
export function formatRupiah(value?: string | number | null): string {
  const numeric = toNumeric(value);
  const formatted = new Intl.NumberFormat("id-ID", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(Math.abs(numeric));
  return `${numeric < 0 ? "−" : ""}Rp ${formatted}`;
}

export function Rupiah({ value, className = "", colorSign = true }: RupiahProps) {
  const numeric = toNumeric(value);

  const isNegative = numeric < 0;
  const absVal = Math.abs(numeric);

  const formatted = new Intl.NumberFormat("id-ID", {
    minimumFractionDigits: 0,
    maximumFractionDigits: 0,
  }).format(absVal);

  const sign = isNegative ? "−" : "";
  const colorClass = colorSign && isNegative ? "text-danger" : "";

  return (
    <span
      className={`num-right tabular whitespace-nowrap ${colorClass} ${className}`.trim()}
    >
      {sign}Rp&nbsp;{formatted}
    </span>
  );
}
