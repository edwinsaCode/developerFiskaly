/**
 * Label unit yang dilihat pemakai.
 *
 * Kode unit sendirian ("C-01") tidak menjawab pertanyaan yang paling sering
 * ditanya di lapangan: unit ini punya siapa. Nama pemegangnya sudah ikut di
 * payload (`buyer_name`, diturunkan backend dari kontrak → booking → buyer_ref),
 * jadi layar tinggal menampilkannya.
 *
 * Fungsi ini sengaja satu-satunya tempat format itu ditentukan — supaya kartu
 * unit, judul halaman, dan tabel penjualan tidak berselisih gaya.
 */

/** "C-01 · Udin" bila ada pemegangnya, "C-01" bila belum. */
export function unitLabel(code: string, buyerName?: string | null): string {
  const buyer = (buyerName ?? "").trim();
  return buyer ? `${code} · ${buyer}` : code;
}

/**
 * Kata untuk peran si pemegang unit, dibaca dari asal namanya — bukan ditebak
 * dari status unit, karena status dan kontrak bisa bergerak terpisah.
 */
export function buyerRoleLabel(source?: string | null): string {
  switch (source) {
    case "booking":
      return "Pemesan";
    case "contract":
    case "unit":
      return "Pembeli";
    default:
      return "Pembeli";
  }
}
