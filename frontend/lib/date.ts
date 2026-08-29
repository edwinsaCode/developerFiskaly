// Utilitas tanggal kalender LOKAL — bukan UTC.
//
// `new Date().toISOString().slice(0, 10)` (pola yang tersebar di puluhan
// form sebelum ini) selalu membaca tanggal UTC. Untuk zona waktu Indonesia
// (WIB/WITA/WIT = UTC+7/+8/+9), itu salah setiap hari selama jam-jam
// pertama waktu lokal (00:00 sampai 07:00/08:00/09:00): tanggal UTC masih
// "kemarin", jadi nilai default "tanggal hari ini" mundur satu hari persis
// pada jam-jam pengguna paling sering membuka aplikasi (pagi hari) — dan
// beberapa layar mengirim default itu langsung sebagai tanggal akuntansi
// (tanggal pembalikan jurnal, tanggal bayar, tanggal akad, dst). Helper di
// bawah selalu membaca komponen tanggal LOKAL browser, tidak pernah lewat
// toISOString().

function pad2(n: number): string {
  return String(n).padStart(2, "0");
}

export function dateToLocalStr(d: Date): string {
  return `${d.getFullYear()}-${pad2(d.getMonth() + 1)}-${pad2(d.getDate())}`;
}

export function todayLocalStr(): string {
  return dateToLocalStr(new Date());
}

export function plusDaysLocalStr(days: number): string {
  const d = new Date();
  d.setDate(d.getDate() + days);
  return dateToLocalStr(d);
}

export function monthStartLocalStr(): string {
  const d = new Date();
  return dateToLocalStr(new Date(d.getFullYear(), d.getMonth(), 1));
}
