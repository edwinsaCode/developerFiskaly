// Nama produk yang dilihat pemakai. Satu tempat, supaya layar tidak pernah
// lagi berselisih dengan layar lain.
//
// Sebelumnya shell menampilkan "NATA ALAM" sementara halaman masuk menampilkan
// "esaProperti" — dua nama untuk satu aplikasi, terlihat langsung oleh klien
// pada layar pertama.
//
// `esaproperti` yang TETAP dipakai adalah identifier teknis: nama module Go,
// nama package npm, nama database, path API. Itu bukan merek dan tidak pernah
// sampai ke mata pemakai; menggantinya berarti rename lintas sistem tanpa satu
// pun manfaat yang terlihat.

export const BRAND_NAME = "NATA ALAM RAYA";

/** Merek dipecah dua supaya kata terakhir bisa diberi warna aksen. */
export const BRAND_PREFIX = "NATA ALAM";
export const BRAND_ACCENT = "RAYA";

export const BRAND_TAGLINE = "Akuntansi Developer Properti";

/** Judul tab browser: "Jurnal — NATA ALAM RAYA". */
export function pageTitle(page: string): string {
  return `${page} — ${BRAND_NAME}`;
}
