---
name: views
description: Show results the owner reads better as a table, list, chart or cards than as prose, and offer next steps as buttons or choices; the app draws the view under your reply. Use for comparisons, version changes, reading lists, timelines, numbers over time and "what next" choices (表格, 对比, 列表, 图表, 按钮, 选项).
metadata:
  acorn-requires-tools: show_view
---

# Views

A view is a small screen under your reply, drawn by the app from a fixed catalog of components. Load the tools with `tool_search({"query":"select:show_view,update_view"})`, then call `show_view` once per view.

## When

- Several items share the same fields (versions, books, options, sources): a Table, or a List of Cards when each item needs a link or more text.
- Numbers across items or over time: a Chart.
- A few clear next steps after this reply: Buttons, or a ChoicePicker when the owner picks one or several of many.
- Everything else stays prose. A one-paragraph answer is never a view.
- The reply still answers. It says what the view shows and adds what the view cannot hold (judgement, caveats, the next step); it does not repeat the rows.
- A question this run cannot continue without goes to `ask_operator`. A view's buttons stay pressable after the run ends, and each press starts a new run.

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
| `Button` | `label` and `action`, both required; `variant`: `primary` (default), `quiet` |
| `ChoicePicker` | `options`: 1 to 20 `{"id": "…", "label": "…"}` with distinct ids, required; `submit`: the label of its send button, required; `action` required; `multiple`: `true` lets the owner pick several; `label` |

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

## Actions

An `action` is `{"name": "save_note", "context": {…}}`. The name starts with a lowercase letter and holds only lowercase letters, digits and `_`; it tells you later what the owner chose. `context` is optional: values written out or bound with paths, sent along with the action. Inside a List's template every item's button is its own action, and its context paths are relative to that item.

When the owner presses a button or sends a choice, the app shows it as their message in this conversation (the button's label, or `选了：A、C`) and a new run starts on it. Beside the message you see where it came from:

```
[Sent from your view "读完之后" (view_…) through save: action save_note]
Context: {"url":"https://example.com/long-read"}
```

with `List items: […]` for a button inside a List and `Chosen: id (label), …` for a choice. Each button and picker is sent once; the app then shows it as sent. Act on the message like any request from the owner.

```json
{
  "title": "读完之后",
  "components": [
    {"id": "root", "component": "Column", "children": ["steps", "pick"]},
    {"id": "steps", "component": "Row", "children": ["save", "skip"]},
    {"id": "save", "component": "Button", "label": "整理成笔记", "action": {"name": "save_note", "context": {"url": {"path": "/url"}}}},
    {"id": "skip", "component": "Button", "label": "先不用", "variant": "quiet", "action": {"name": "skip"}},
    {"id": "pick", "component": "ChoicePicker", "label": "还想做什么", "options": {"path": "/more"}, "multiple": true, "submit": "就这些", "action": {"name": "follow_up", "context": {"url": {"path": "/url"}}}}
  ],
  "data": {
    "url": "https://example.com/long-read",
    "more": [{"id": "summary", "label": "写一段摘要"}, {"id": "remind", "label": "周末提醒我再看"}, {"id": "related", "label": "找相关的文章"}]
  }
}
```

## Updating a view

`update_view` takes the `view_id` that `show_view` returned and replaces the view's title, components or data, each given whole; what you leave out stays. The changed view is checked like a new one and the app redraws it where it is. Use it when the view's content changes, for example a status Badge after the work it offered is done. Actions the owner already sent stay sent.

## When it fails

A view that fails its checks is not shown. The result lists each problem with the component id and property, for example a path that finds nothing in `data` or a row with the wrong number of cells. Fix all of them and call `show_view` or `update_view` again.
