import { Suspense } from "react";

import { PublicPage } from "@/components/app-shell";
import { LoginForm } from "@/features/auth/login-form";

export default function LoginPage() {
  return (
    <PublicPage>
      <Suspense>
        <LoginForm />
      </Suspense>
    </PublicPage>
  );
}
