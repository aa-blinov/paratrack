import * as React from "react"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { FieldIssue, FormIssue } from "@/components/form-issue"
import { StatusLine } from "@/components/status-line"
import { translate as t } from "@/i18n"
import { apiJSON, issueOf, type Issue } from "@/lib/json-api"
import type { Client } from "@/lib/team-directory"
import type { Contact } from "@/crm/model"

const contactFields: Record<string, string> = { "contact name": "name", client: "client" }

/** The people at a client. Deals are linked to one of them; the list is kept per client. */
export function ContactsPanel({ lang, csrf, clients }: { lang: string; csrf: string; clients: Client[] }) {
  const [picked, setPicked] = React.useState("")
  const clientID = picked || (clients[0] ? String(clients[0].id) : "")
  const [contacts, setContacts] = React.useState<Contact[] | null>(null)
  const [name, setName] = React.useState("")
  const [role, setRole] = React.useState("")
  const [email, setEmail] = React.useState("")
  const [phone, setPhone] = React.useState("")
  const [issue, setIssue] = React.useState<Issue | null>(null)
  // A failed save of an edit shows under that edit, not under the form that adds a contact.
  const [editIssue, setEditIssue] = React.useState<Issue | null>(null)
  const [busy, setBusy] = React.useState(false)
  const [saved, setSaved] = React.useState(false)
  const [editingID, setEditingID] = React.useState<number | null>(null)
  const [editName, setEditName] = React.useState("")
  const [editRole, setEditRole] = React.useState("")
  const [editEmail, setEditEmail] = React.useState("")
  const [editPhone, setEditPhone] = React.useState("")

  const load = React.useCallback(async (id: string) => {
    if (!id) {
      setContacts([])
      return
    }
    try {
      const response = await apiJSON<{ contacts: Contact[] }>(`/api/crm/contacts?client_id=${id}`, "GET", "")
      setContacts(response.contacts)
    } catch (cause) {
      setIssue(issueOf(cause, {}, t(lang, "err.generic")))
    }
  }, [lang])

  React.useEffect(() => { void load(clientID) }, [clientID, load])

  function startEdit(contact: Contact) {
    setEditingID(contact.id)
    setEditName(contact.name)
    setEditRole(contact.role)
    setEditEmail(contact.email)
    setEditPhone(contact.phone)
    setEditIssue(null)
  }

  async function saveEdit(contact: Contact) {
    setBusy(true)
    setEditIssue(null)
    try {
      await apiJSON(`/api/crm/contacts/${contact.id}`, "POST", csrf, { name: editName.trim(), role: editRole.trim(), email: editEmail.trim(), phone: editPhone.trim() })
      setEditingID(null)
      await load(clientID)
    } catch (cause) {
      setEditIssue(issueOf(cause, contactFields, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  async function remove(contact: Contact) {
    setBusy(true)
    setIssue(null)
    setSaved(false)
    try {
      await apiJSON(`/api/crm/contacts/${contact.id}/delete`, "POST", csrf)
      await load(clientID)
    } catch (cause) {
      setIssue(issueOf(cause, {}, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  async function add(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault()
    setBusy(true)
    setIssue(null)
    setSaved(false)
    try {
      await apiJSON("/api/crm/contacts", "POST", csrf, { client_id: Number(clientID), name: name.trim(), role: role.trim(), email: email.trim(), phone: phone.trim() })
      setName("")
      setRole("")
      setEmail("")
      setPhone("")
      setSaved(true)
      await load(clientID)
    } catch (cause) {
      setIssue(issueOf(cause, contactFields, t(lang, "err.generic")))
    } finally {
      setBusy(false)
    }
  }

  return <Card>
    <CardHeader><CardTitle>{t(lang, "crm.contacts")}</CardTitle></CardHeader>
    <CardContent className="grid gap-5">
      {clients.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "crm.noClients")}</p> : <>
        <div className="grid gap-2 sm:max-w-sm">
          <Label htmlFor="contacts-client">{t(lang, "crm.client")}</Label>
          <Select value={clientID} onValueChange={setPicked}>
            <SelectTrigger id="contacts-client" className="w-full" style={{ fontSize: 16 }}><SelectValue /></SelectTrigger>
            <SelectContent>{clients.map(client => <SelectItem key={client.id} value={String(client.id)}>{client.name}</SelectItem>)}</SelectContent>
          </Select>
          <FieldIssue issue={issue} name="client" id="contacts-client-issue" />
        </div>
        {contacts === null ? null : contacts.length === 0 ? <p className="text-sm text-muted-foreground">{t(lang, "crm.noContacts")}</p> : <ul className="grid divide-y rounded-lg border">
          {contacts.map(contact => editingID === contact.id
            ? <li key={contact.id} className="grid gap-3 p-3 text-sm sm:grid-cols-2" aria-busy={busy}>
              <FormIssue issue={editIssue} />
              <div className="grid gap-2">
                <Label htmlFor={`contact-edit-name-${contact.id}`}>{t(lang, "crm.contactName")}</Label>
                <Input id={`contact-edit-name-${contact.id}`} value={editName} onChange={event => setEditName(event.target.value)} required maxLength={200} aria-invalid={editIssue?.key === "name" || undefined} style={{ fontSize: 16 }} />
                <FieldIssue issue={editIssue} name="name" id={`contact-edit-name-${contact.id}-issue`} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor={`contact-edit-role-${contact.id}`}>{t(lang, "crm.contactRole")}</Label>
                <Input id={`contact-edit-role-${contact.id}`} value={editRole} onChange={event => setEditRole(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor={`contact-edit-email-${contact.id}`}>{t(lang, "crm.contactEmail")}</Label>
                <Input id={`contact-edit-email-${contact.id}`} type="email" value={editEmail} onChange={event => setEditEmail(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor={`contact-edit-phone-${contact.id}`}>{t(lang, "crm.contactPhone")}</Label>
                <Input id={`contact-edit-phone-${contact.id}`} type="tel" value={editPhone} onChange={event => setEditPhone(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
              </div>
              <div className="flex flex-wrap gap-2 sm:col-span-2">
                <Button type="button" size="sm" disabled={busy} onClick={() => void saveEdit(contact)}>{t(lang, "crm.contactSave")}</Button>
                <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => { setEditingID(null); setEditIssue(null) }}>{t(lang, "projects.cancel")}</Button>
              </div>
            </li>
            : <li key={contact.id} className="flex items-start justify-between gap-3 p-3 text-sm">
              <span className="grid min-w-0 gap-0.5">
                <span className="font-medium [overflow-wrap:anywhere]">{contact.name}{contact.role && <span className="font-normal text-muted-foreground">, {contact.role}</span>}</span>
                {(contact.email || contact.phone) && <span className="text-muted-foreground [overflow-wrap:anywhere]">{[contact.email, contact.phone].filter(Boolean).join(", ")}</span>}
              </span>
              {(contact.can_edit || contact.can_delete) && <span className="flex shrink-0 gap-1">
                {contact.can_edit && <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => startEdit(contact)}>{t(lang, "crm.contactEdit")}</Button>}
                {contact.can_delete && <Button type="button" size="sm" variant="ghost" disabled={busy} onClick={() => void remove(contact)}>{t(lang, "crm.contactDelete")}</Button>}
              </span>}
            </li>)}
        </ul>}
        <form onSubmit={add} className="grid gap-4 sm:grid-cols-2" aria-busy={busy}>
          <FormIssue issue={issue} />
          <div className="grid gap-2">
            <Label htmlFor="contact-name">{t(lang, "crm.contactName")}</Label>
            <Input id="contact-name" value={name} onChange={event => setName(event.target.value)} required maxLength={200} aria-invalid={issue?.key === "name" || undefined} aria-describedby={issue?.key === "name" ? "contact-name-issue" : undefined} style={{ fontSize: 16 }} />
            <FieldIssue issue={issue} name="name" id="contact-name-issue" />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="contact-role">{t(lang, "crm.contactRole")}</Label>
            <Input id="contact-role" value={role} onChange={event => setRole(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="contact-email">{t(lang, "crm.contactEmail")}</Label>
            <Input id="contact-email" type="email" value={email} onChange={event => setEmail(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="contact-phone">{t(lang, "crm.contactPhone")}</Label>
            <Input id="contact-phone" type="tel" value={phone} onChange={event => setPhone(event.target.value)} maxLength={200} style={{ fontSize: 16 }} />
          </div>
          <div className="flex flex-wrap items-center gap-3 sm:col-span-2">
            <Button type="submit" disabled={busy || !clientID}>{t(lang, "crm.addContact")}</Button>
            <StatusLine text={saved ? t(lang, "crm.saved") : ""} />
          </div>
        </form>
      </>}
    </CardContent>
  </Card>
}
