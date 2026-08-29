/**
 * Cari & halaman untuk tabel — logika murni, tanpa React.
 *
 * Dipisah dari komponennya supaya perilaku yang gampang salah (halaman jadi
 * kosong setelah difilter, "menampilkan 21–40 dari 12") bisa diuji tanpa DOM.
 *
 * Semua tabel di aplikasi ini memfilter di sisi klien: datanya sudah dimuat
 * penuh oleh halaman server. Kalau nanti ada tabel yang datanya jutaan baris,
 * pindahkan filternya ke API — jangan besarkan file ini.
 */

/** Buang beda huruf besar/kecil dan spasi berlebih supaya "  Budi " == "budi". */
export function normalize(value: unknown): string {
  return String(value ?? "")
    .toLowerCase()
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * Cocok bila SETIAP kata kunci muncul di salah satu kolom yang dicari.
 *
 * Per kata, bukan per kalimat: orang mengetik "budi c-01" untuk mempersempit
 * dua kolom sekaligus, dan pencarian yang menuntut urutan persis akan menjawab
 * "tidak ada" untuk baris yang jelas-jelas ia maksud.
 */
export function matchesQuery(fields: unknown[], query: string): boolean {
  const terms = normalize(query).split(" ").filter(Boolean);
  if (terms.length === 0) return true;
  const hay = fields.map(normalize).filter(Boolean);
  return terms.every((term) => hay.some((h) => h.includes(term)));
}

/** Jumlah halaman; nol baris tetap satu halaman (kosong), bukan nol halaman. */
export function pageCount(total: number, pageSize: number): number {
  if (pageSize <= 0) return 1;
  return Math.max(1, Math.ceil(total / pageSize));
}

/**
 * Jaga `page` tetap di dalam rentang yang ada.
 *
 * Ini yang mencegah "tabel mendadak kosong": pemakai di halaman 5 lalu
 * mengetik kata pencarian yang menyisakan 3 baris — tanpa penjepitan ini ia
 * melihat tabel kosong dan menyimpulkan datanya hilang.
 */
export function clampPage(page: number, total: number, pageSize: number): number {
  const last = pageCount(total, pageSize);
  if (!Number.isFinite(page) || page < 1) return 1;
  return Math.min(Math.floor(page), last);
}

/** Potongan baris untuk satu halaman (halaman mulai dari 1). */
export function pageSlice<T>(rows: T[], page: number, pageSize: number): T[] {
  if (pageSize <= 0) return rows;
  const p = clampPage(page, rows.length, pageSize);
  const start = (p - 1) * pageSize;
  return rows.slice(start, start + pageSize);
}

/** "Menampilkan 21–40 dari 137" — angka pertama/terakhir yang benar-benar tampil. */
export function rangeLabel(page: number, pageSize: number, total: number): string {
  if (total === 0) return "Tidak ada baris";
  const p = clampPage(page, total, pageSize);
  const from = (p - 1) * pageSize + 1;
  const to = Math.min(p * pageSize, total);
  return `Menampilkan ${from}–${to} dari ${total}`;
}
