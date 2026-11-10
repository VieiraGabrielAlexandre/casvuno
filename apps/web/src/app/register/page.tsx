import { Suspense } from "react";

import { PublicPage } from "@/components/app-shell";
import { RegisterForm } from "@/features/auth/register-form";

export default function RegisterPage() {
  return (
    <PublicPage>
      <Suspense>
        <RegisterForm />
      </Suspense>
    </PublicPage>
  );
}
