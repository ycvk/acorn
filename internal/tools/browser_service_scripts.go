package tools

import (
	"fmt"
	"strconv"
)

type snapshotElementRaw struct {
	Selector     string `json:"selector"`
	Role         string `json:"role"`
	Name         string `json:"name"`
	ValuePreview string `json:"value_preview"`
	Enabled      bool   `json:"enabled"`
	Visible      bool   `json:"visible"`
}

func selectorCountScript(selector string) string {
	selectorJSON := strconv.Quote(selector)
	return fmt.Sprintf(`(() => document.querySelectorAll(%s).length)()`, selectorJSON)
}

func actionScript(action, selector, value string) string {
	selectorJSON := strconv.Quote(selector)
	valueJSON := strconv.Quote(value)
	return fmt.Sprintf(actionScriptTemplate, selectorJSON, action, valueJSON, valueJSON, action, valueJSON)
}

const actionScriptTemplate = `(() => {
  const matches = document.querySelectorAll(%s);
  if (matches.length !== 1) throw new Error("selector must match exactly one element");
  const el = matches[0];
  if (%q === "fill") {
    if (el.isContentEditable) {
      el.textContent = %s;
    } else if ("value" in el) {
      el.value = %s;
    } else {
      throw new Error("target is not fillable");
    }
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  }
  if (%q === "select") {
    if (!(el instanceof HTMLSelectElement)) throw new Error("target is not a select element");
    const wanted = %s;
    const option = Array.from(el.options).find((candidate) =>
      candidate.value === wanted || candidate.label === wanted || candidate.text === wanted
    );
    if (!option) throw new Error("select option not found");
    el.value = option.value;
    el.dispatchEvent(new Event("input", { bubbles: true }));
    el.dispatchEvent(new Event("change", { bubbles: true }));
    return true;
  }
  throw new Error("unsupported browser action");
})()`

func snapshotScript(limit int) string {
	if limit <= 0 {
		limit = defaultElementLimit
	}
	return fmt.Sprintf(snapshotScriptTemplate, limit)
}

const snapshotScriptTemplate = `(() => {
  const limit = %d;
  const candidates = Array.from(document.querySelectorAll([
    "a[href]",
    "button",
    "input",
    "textarea",
    "select",
    "[role=button]",
    "[role=link]",
    "[contenteditable=true]",
    "[tabindex]:not([tabindex='-1'])"
  ].join(",")));
  const cssEscape = (value) => {
    if (window.CSS && CSS.escape) return CSS.escape(value);
    return String(value).replace(/[^a-zA-Z0-9_-]/g, "\\$&");
  };
  const visible = (el) => {
    const style = window.getComputedStyle(el);
    const rect = el.getBoundingClientRect();
    return style.display !== "none" && style.visibility !== "hidden" && Number(style.opacity) !== 0 && rect.width > 0 && rect.height > 0;
  };
  const roleFor = (el) => {
    const explicit = el.getAttribute("role");
    if (explicit) return explicit;
    const tag = el.tagName.toLowerCase();
    if (tag === "a") return "link";
    if (tag === "button") return "button";
    if (tag === "select") return "combobox";
    if (tag === "textarea") return "textbox";
    if (tag === "input") {
      const type = (el.getAttribute("type") || "text").toLowerCase();
      if (type === "checkbox") return "checkbox";
      if (type === "radio") return "radio";
      if (type === "submit" || type === "button") return "button";
      return "textbox";
    }
    return tag;
  };
  const nameFor = (el) => {
    const aria = el.getAttribute("aria-label");
    if (aria) return aria.trim();
    const labelledBy = el.getAttribute("aria-labelledby");
    if (labelledBy) {
      const text = labelledBy.split(/\s+/).map((id) => document.getElementById(id)?.innerText || "").join(" ").trim();
      if (text) return text;
    }
    if (el.labels && el.labels.length) {
      const label = Array.from(el.labels).map((item) => item.innerText || "").join(" ").trim();
      if (label) return label;
    }
    if (el.title) return el.title.trim();
    if (el.placeholder) return el.placeholder.trim();
    return (el.innerText || el.value || "").trim();
  };
  const selectorFor = (el) => {
    if (el.id) {
      const selector = "#" + cssEscape(el.id);
      if (document.querySelectorAll(selector).length === 1) return selector;
    }
    const parts = [];
    let node = el;
    while (node && node.nodeType === Node.ELEMENT_NODE && node !== document.documentElement) {
      let part = node.tagName.toLowerCase();
      const parent = node.parentElement;
      if (!parent) break;
      const sameTag = Array.from(parent.children).filter((child) => child.tagName === node.tagName);
      if (sameTag.length > 1) {
        const index = sameTag.indexOf(node) + 1;
        part += ":nth-of-type(" + index + ")";
      }
      parts.unshift(part);
      const selector = parts.join(" > ");
      if (document.querySelectorAll(selector).length === 1) return selector;
      node = parent;
    }
    return parts.join(" > ");
  };
  const out = [];
  for (const el of candidates) {
    if (out.length >= limit) break;
    const selector = selectorFor(el);
    if (!selector) continue;
    const disabled = Boolean(el.disabled) || el.getAttribute("aria-disabled") === "true";
    let value = "";
    if ("value" in el && typeof el.value === "string") value = el.value;
    out.push({
      selector,
      role: roleFor(el),
      name: nameFor(el).slice(0, 160),
      value_preview: value.slice(0, 120),
      enabled: !disabled,
      visible: visible(el)
    });
  }
  return out;
})()`
