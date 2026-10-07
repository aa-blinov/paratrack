import { useEffect, useRef, useState } from "react"
import { AlertDialog as Primitive } from "radix-ui"
import { Button } from "@/components/ui/button"
import { translate as t } from "@/i18n"
type Request = {message:string;resolve:(accepted:boolean)=>void;trigger:HTMLElement|null}
export function requestConfirmation(message:string) {
  return new Promise<boolean>(resolve=>document.dispatchEvent(new CustomEvent("paratrack:confirm",{detail:{message,resolve,trigger:document.activeElement}})))
}
export function ConfirmationDialog({lang}: {lang:string}) {
  const trigger=useRef<HTMLElement|null>(null)
  const [request,setRequest]=useState<Request|null>(null)
  useEffect(()=>{const listener=(event:Event)=>setRequest(current=>{if(current) current.resolve(false);return (event as CustomEvent<Request>).detail});document.addEventListener("paratrack:confirm",listener);return()=>document.removeEventListener("paratrack:confirm",listener)},[])
  function finish(accepted:boolean) {trigger.current=request?.trigger || null;request?.resolve(accepted);setRequest(null)}
  return <Primitive.Root open={!!request} onOpenChange={open=>{if(!open)finish(false)}}><Primitive.Portal container={document.getElementById("paratrack-react-root") ?? undefined}>
    <Primitive.Overlay className="fixed inset-0 z-50 bg-black/50"/>
    <Primitive.Content onCloseAutoFocus={event=>{event.preventDefault();trigger.current?.focus()}} className="fixed left-1/2 top-1/2 z-50 grid max-h-[calc(100svh-2rem)] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 grid-rows-[auto_minmax(0,1fr)_auto] gap-4 rounded-xl border bg-background p-6 shadow-lg">
      <Primitive.Title className="text-lg font-semibold">{t(lang,"common.confirmTitle")}</Primitive.Title>
      <Primitive.Description className="min-h-0 overflow-y-auto text-sm text-muted-foreground [overflow-wrap:anywhere]">{request?.message}</Primitive.Description>
      <div className="flex justify-end gap-2"><Primitive.Cancel asChild><Button variant="outline" onClick={()=>finish(false)}>{t(lang,"projects.cancel")}</Button></Primitive.Cancel><Primitive.Action asChild><Button variant="destructive" onClick={()=>finish(true)}>{t(lang,"common.confirmAction")}</Button></Primitive.Action></div>
    </Primitive.Content>
  </Primitive.Portal></Primitive.Root>
}
