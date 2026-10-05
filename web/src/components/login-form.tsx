import { LogIn } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card"
import { Field, FieldDescription, FieldGroup, FieldLabel } from "@/components/ui/field"
import { Input } from "@/components/ui/input"
import { translate as t } from "@/i18n"
import type { AuthPageData } from "@/dashboard/types"

function nextHref(path: string, next: string) {
  return next ? `${path}?next=${encodeURIComponent(next)}` : path
}

export function LoginForm({ data }: { data: AuthPageData }) {
  const lang = data.Lang || "en"
  return <Card className="w-full">
    <CardHeader className="space-y-1 text-center">
      <h1 className="text-2xl font-semibold tracking-tight">{t(lang, "auth.welcomeBack")}</h1>
      <CardDescription>{t(lang, "auth.signInContinue")}</CardDescription>
    </CardHeader>
    <CardContent className="grid gap-4">
      {data.ErrorMsg && <div role="alert" className="rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm text-destructive">{data.ErrorMsg}</div>}
      <form method="post" action="/api/login">
        <input type="hidden" name="csrf_token" value={data.CSRFToken} />
        <input type="hidden" name="next" value={data.Next} />
        <FieldGroup>
          <Field>
            <FieldLabel htmlFor="email">{t(lang, "auth.email")}</FieldLabel>
            <Input id="email" type="email" name="email" required autoComplete="email" defaultValue={data.Email} autoFocus />
          </Field>
          <Field>
            <div className="flex items-center justify-between gap-3">
              <FieldLabel htmlFor="password">{t(lang, "auth.password")}</FieldLabel>
              <a href="/forgot-password" className="text-sm text-muted-foreground underline-offset-4 hover:text-foreground hover:underline">{t(lang, "auth.forgot")}</a>
            </div>
            <Input id="password" type="password" name="password" required autoComplete="current-password" />
          </Field>
          <Field>
            <Button type="submit" className="w-full"><LogIn aria-hidden="true" />{t(lang, "auth.signIn")}</Button>
            {data.SSO && <Button asChild variant="outline" className="w-full"><a href="/sso/login">{t(lang, "auth.sso")}</a></Button>}
            <FieldDescription className="text-center">{t(lang, "auth.noAccount")} <a href={nextHref("/register", data.Next)}>{t(lang, "nav.signup")}</a></FieldDescription>
          </Field>
        </FieldGroup>
      </form>
    </CardContent>
  </Card>
}
