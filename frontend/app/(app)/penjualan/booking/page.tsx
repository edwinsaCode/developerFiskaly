import { cookies } from "next/headers";
import { BookingBoard } from "@/components/booking/BookingBoard";

const COOKIE_NAME = "esa_session";

export default async function BookingPage() {
  const store = await cookies();
  const token = store.get(COOKIE_NAME)?.value ?? "";

  return (
    <div>
      <BookingBoard token={token} />
    </div>
  );
}
