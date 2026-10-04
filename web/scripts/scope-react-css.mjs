import { readFile, writeFile } from "node:fs/promises"
import postcss from "postcss"
import selectorParser from "postcss-selector-parser"

const path = new URL("../../internal/web/static/ui/app.css", import.meta.url)
const rootSelector = "#paratrack-react-root"
const css = await readFile(path, "utf8")
const ast = postcss.parse(css, { from: path.pathname })
let scoped = 0

function insideKeyframes(rule) {
  let parent = rule.parent
  while (parent) {
    if (parent.type === "atrule" && /keyframes$/i.test(parent.name)) return true
    parent = parent.parent
  }
  return false
}

function hasRoot(selector) {
  return selector.nodes.some(node => node.type === "id" && node.value === "paratrack-react-root")
}

ast.walkRules(rule => {
  if (insideKeyframes(rule)) return
  rule.selector = selectorParser(selectors => {
    selectors.each(selector => {
      if (hasRoot(selector)) return

      // Tailwind's theme variant matches descendants of the document theme,
      // so the root prefix belongs before its selector.
      const theme = selector.nodes.find(node =>
        node.type === "pseudo" && node.value === ":where" && node.toString().includes("data-theme=paratrack-dark"),
      )
      const id = selectorParser.id({ value: "paratrack-react-root" })
      const descendant = selectorParser.combinator({ value: " " })
      if (theme) {
        selector.prepend(descendant)
        selector.prepend(id)
        scoped++
        return
      }

      const first = selector.nodes[0]
      if (first?.type === "pseudo" && [":root", ":host"].includes(first.value)) {
        first.replaceWith(id)
      } else if (first?.type === "tag" && ["html", "body"].includes(first.value.toLowerCase())) {
        first.replaceWith(id)
      } else {
        selector.prepend(descendant)
        selector.prepend(id)
      }
      scoped++
    })
  }).processSync(rule.selector)
})

await writeFile(path, ast.toString())
process.stdout.write(`Scoped ${scoped} React CSS selectors under ${rootSelector}\n`)
