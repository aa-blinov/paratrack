import { Children, useCallback, useEffect, useRef, useState, type ReactNode } from "react"
import { flushSync } from "react-dom"
import { Collapsible as Primitive } from "radix-ui"

export function openDisclosure(id:string) {document.getElementById(id)?.dispatchEvent(new Event("disclosure:open"))}
export function Disclosure({children,open=false,onOpenChange,id,className}: {children:ReactNode;open?:boolean;onOpenChange?:(open:boolean)=>void;id?:string;className?:string}) {
  const [expanded,setExpanded]=useState(open)
  const ref=useRef<HTMLDivElement>(null)
  useEffect(()=>setExpanded(open),[open])
  // The trigger only tells the parent; the parent's reply is the open prop.
  // That keeps a lifted owner (the "forgot to start?" hint on the dashboard)
  // the single source of truth, while an uncontrolled owner keeps its own
  // state through the onOpenChange callback.
  const change=useCallback((value:boolean)=>onOpenChange?.(value),[onOpenChange])
  const shown=Boolean(onOpenChange?open:expanded)
  useEffect(()=>{const el=ref.current;const show=()=>change(true);el?.addEventListener("disclosure:open",show);return()=>el?.removeEventListener("disclosure:open",show)},[change])
  const [trigger,...content]=Children.toArray(children)
  return <Primitive.Root ref={ref} id={id} data-disclosure open={shown} onOpenChange={change} className={className} onInvalidCapture={()=>flushSync(()=>change(true))}>
    {trigger}<Primitive.Content forceMount hidden={!shown}>{content}</Primitive.Content>
  </Primitive.Root>
}
export function DisclosureTrigger({children,className}: {children:ReactNode;className?:string}) {
  return <Primitive.Trigger type="button" className={`w-full text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring ${className || ""}`}>{children}</Primitive.Trigger>
}
