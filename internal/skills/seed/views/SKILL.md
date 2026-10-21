---
name: views
description: Show results the owner reads better as a table, list, chart or cards than as prose; the app draws the view under your reply. Use for comparisons, version changes, reading lists, timelines and numbers over time (表格, 对比, 列表, 图表).
metadata:
  acorn-requires-tools: show_view
---

# Views

A view is a small screen under your reply, drawn by the app from a fixed catalog of components. Load the tool with `tool_search({"query":"select:show_view"})`, then call `show_view` once per view.

## When

- Several items share the same fields (versions, books, options, sources): a Table, or a List of Cards when each item needs a link or more text.
- Numbers across items or over time: a Chart.
- Everything else stays prose. A one-paragraph answer is never a view.
- The reply still answers. It says what the view shows and adds what the view cannot hold (judgement, caveats, the next step); it does not repeat the rows.

## Shape

- `components` is flat. Each has an `id` (letters, digits, `_`, `-`) and a `component` type. Containers list their children's ids in `children`.
- Exactly one component has id `root`. Every other component sits in exactly one container, or is a List's `template`.
- A value is written out, or `{"path": "/releases"}`, a JSON Pointer into `data`. Inside a List's template a path without the leading slash is relative to the item: `{"path": "title"}`; `{"path": ""}` is the item itself.
- Put repeated content in `data` and point at it, so the components stay few.
- The app chooses colours, sizes and spacing; components carry content and meaning only.

## Catalog `acorn.views.v1`

| component | properties |
|---|---|
| `Column`, `Row`, `Card` | `children`: ids, required |
| `Divider` | none |
| `Text` | `text` required, inline Markdown links allowed; `variant`: `body` (default), `title`, `caption` |
| `Badge` | `text` required; `tone`: `neutral` (default), `accent`, `success`, `warning`, `danger` |
| `Link` | `label` and `url`, both required; http(s) addresses the web policy allows |
| `Image` | `file` required: a file id of this conversation (`file_…`) or a knowledge attachment path (`attachments/…`); `alt` |
| `Table` | `columns`: 1 to 8 headings, written out; `rows`: an array of rows, each with one cell per column (string, number or null) |
| `List` | `items`: `{"path": …}` to an array; `template`: id of the component drawn for each item |
| `Chart` | `variant`: `line` or `bar`, required; `points`: `[{"label": "…", "value": number}]`; `unit` |

Limits: 200 components, 64 KiB, a one-line title of at most 80 characters, texts of at most 2000 characters, at most 100 rows, items or points.

## Example

```json
{
  "title": "eino v0.9.17 → v0.9.21",
  "components": [
    {"id": "root", "component": "Column", "children": ["lead", "table", "notes"]},
    {"id": "lead", "component": "Text", "variant": "caption", "text": "来自 [GitHub releases](https://github.com/cloudwego/eino/releases)"},
    {"id": "table", "component": "Table", "columns": ["版本", "日期", "要点"], "rows": {"path": "/releases"}},
    {"id": "notes", "component": "List", "items": {"path": "/notes"}, "template": "note"},
    {"id": "note", "component": "Card", "children": ["note_title", "note_link"]},
    {"id": "note_title", "component": "Text", "text": {"path": "title"}},
    {"id": "note_link", "component": "Link", "label": "读原文", "url": {"path": "url"}}
  ],
  "data": {
    "releases": [["v0.9.21", "10-15", "summarization 可以只清理不压缩"], ["v0.9.20", "10-08", "middleware 改为显式顺序"]],
    "notes": [{"title": "v0.9.21 release notes", "url": "https://github.com/cloudwego/eino/releases/tag/v0.9.21"}]
  }
}
```

## When it fails

A view that fails its checks is not shown. The result lists each problem with the component id and property, for example a path that finds nothing in `data` or a row with the wrong number of cells. Fix all of them and call `show_view` again.
