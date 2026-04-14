// Command playground runs a web-based SPL template playground.
// It provides a browser UI with Monaco editors for template + data JSON,
// renders templates server-side, and shows live HTML preview.
//
// Usage:
//
//	go run ./cmd/playground
//
// Then visit http://localhost:8090
package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	template "github.com/oarkflow/template"
)

//go:embed static/*
var staticFS embed.FS

type renderRequest struct {
	Template string `json:"template"`
	Data     string `json:"data"`
}

type renderResponse struct {
	Result     string `json:"result"`
	ResultType string `json:"result_type"`
	Error      string `json:"error"`
	ErrorKind  string `json:"error_kind"`
	DurationMS int64  `json:"duration_ms"`
}

func builtinExamples() []map[string]any {
	return []map[string]any{
		{
			"name":     "basic",
			"label":    "Basic Expressions",
			"category": "Core Templates",
			"tags":     []string{"expressions", "html"},
			"template": "<h1>${title}</h1>\n<p>${message}</p>\n<p>By ${author | upper}</p>",
			"data":     `{"title": "Hello SPL", "message": "Welcome to SPL Templates!", "author": "alice"}`,
		},
		{
			"name":     "conditionals",
			"label":    "Conditionals",
			"category": "Core Templates",
			"tags":     []string{"if", "branches"},
			"template": "@if(isLoggedIn) {\n  <p>Welcome back, ${user}!</p>\n} @elseif(showGuest) {\n  <p>Hello, guest!</p>\n} @else {\n  <p>Please sign in.</p>\n}",
			"data":     `{"isLoggedIn": true, "user": "Alice", "showGuest": false}`,
		},
		{
			"name":     "loop",
			"label":    "Loops",
			"category": "Core Templates",
			"tags":     []string{"for", "empty"},
			"template": "<h2>Shopping List</h2>\n<ul>\n@for(item in items) {\n  <li>${item}</li>\n}\n</ul>\n@for(x in empty) {\n  <li>${x}</li>\n} @empty {\n  <p><em>The second list is empty.</em></p>\n}",
			"data":     `{"items": ["Apples", "Bread", "Cheese", "Milk"], "empty": []}`,
		},
		{
			"name":     "loop-index",
			"label":    "Loop with Index",
			"category": "Core Templates",
			"tags":     []string{"for", "loop"},
			"template": "<ol>\n@for(i, color in colors) {\n  <li>#${i + 1}: ${color | title} (first=${loop.first}, last=${loop.last})</li>\n}\n</ol>",
			"data":     `{"colors": ["red", "green", "blue", "yellow"]}`,
		},
		{
			"name":     "switch",
			"label":    "Switch",
			"category": "Core Templates",
			"tags":     []string{"switch", "case"},
			"template": "<style>\n.pending { color: orange; }\n.shipped { color: blue; }\n.delivered { color: green; }\n.unknown { color: gray; }\n</style>\n<h2>Order Status</h2>\n@switch(status) {\n  @case(\"pending\") {\n  <span class=\"pending\">Pending</span>\n  }\n  @case(\"shipped\", \"in_transit\") {\n  <span class=\"shipped\">In Transit</span>\n  }\n  @case(\"delivered\") {\n  <span class=\"delivered\">Delivered</span>\n  }\n  @default {\n  <span class=\"unknown\">Unknown: ${status}</span>\n  }\n}",
			"data":     `{"status": "shipped"}`,
		},
		{
			"name":     "filters",
			"label":    "Filters",
			"category": "Core Templates",
			"tags":     []string{"filters", "formatting"},
			"template": "<p>Upper: ${name | upper}</p>\n<p>Slug: ${title | slug}</p>\n<p>Truncate: ${bio | truncate 25 \"...\"}</p>\n<p>URL: ${query | urlencode}</p>\n<p>Reverse: ${name | reverse}</p>\n<p>Title: ${greeting | title}</p>\n<p>Pad: ${id | padstart 8 \"0\"}</p>",
			"data":     `{"name": "alice", "title": "Hello World!", "bio": "A software engineer who loves building template engines", "query": "spl template engine", "greeting": "hello world from spl", "id": "42"}`,
		},
		{
			"name":     "escaping",
			"label":    "HTML Escaping",
			"category": "Core Templates",
			"tags":     []string{"escaping", "security"},
			"template": "<h2>Auto Escaping Demo</h2>\n<p>Escaped (safe): ${userInput}</p>\n<p>Raw (dangerous): ${raw userInput}</p>\n<p>Escaped code: ${codeSnippet}</p>",
			"data":     `{"userInput": "<script>alert('xss')</script>", "codeSnippet": "<div class=\"test\">Hello & welcome</div>"}`,
		},
		{
			"name":     "full-page",
			"label":    "Full Page",
			"category": "Core Templates",
			"tags":     []string{"page", "layout"},
			"template": "<!DOCTYPE html>\n<html>\n<head>\n  <title>${title}</title>\n  <style>body{font-family:sans-serif;max-width:600px;margin:2rem auto} .item{padding:0.5rem;border-bottom:1px solid #eee}</style>\n</head>\n<body>\n  <h1>${title}</h1>\n  <p>${description | capitalize}</p>\n\n  @if(items) {\n  <h2>Items (${itemCount} total)</h2>\n  @for(item in items) {\n  <div class=\"item\">\n    <strong>${item.name}</strong> - $${item.price}\n    @if(item.onSale) {\n    <span style=\"color:red\"> SALE!</span>\n    }\n  </div>\n  }\n  } @else {\n  <p>No items available.</p>\n  }\n\n  @switch(theme) {\n    @case(\"dark\") {\n  <p style=\"color:#ccc;background:#333;padding:1rem\">Dark theme active</p>\n    }\n    @case(\"light\") {\n  <p style=\"background:#f0f0f0;padding:1rem\">Light theme active</p>\n    }\n    @default {\n  <p>Default theme</p>\n    }\n  }\n\n  @// This comment won't appear in output\n  <footer><small>Rendered by SPL Template Engine</small></footer>\n</body>\n</html>",
			"data":     `{"title": "My Shop", "description": "a demo of all template features", "itemCount": 3, "items": [{"name": "Widget", "price": 9.99, "onSale": true}, {"name": "Gadget", "price": 24.50, "onSale": false}, {"name": "Doohickey", "price": 4.99, "onSale": true}], "theme": "dark"}`,
		},
		{
			"name":     "component-basic",
			"label":    "Components: Basic",
			"category": "Components",
			"tags":     []string{"components", "render"},
			"template": "@// Define reusable components with declared props\n@component(\"Badge\", label, color) {\n  <style>.badge { display: inline-block; padding: 2px 8px; border-radius: 12px; font-size: 12px; color: white; background: ${color | default \"#666\"}; }</style>\n  <span class=\"badge\">${label}</span>\n}\n\n@component(\"Card\", title, body, tag, tagColor) {\n  <style>.card { border: 1px solid #ddd; border-radius: 8px; padding: 16px; margin: 8px 0; }</style>\n  <div class=\"card\">\n    <h3>${title} @render(\"Badge\", {\"label\": tag, \"color\": tagColor})</h3>\n    <p>${body}</p>\n  </div>\n}\n\n<h1>Component Demo</h1>\n@render(\"Card\", {\"title\": \"Getting Started\", \"body\": \"Install SPL and run your first script.\", \"tag\": \"New\", \"tagColor\": \"#22c55e\"})\n@render(\"Card\", {\"title\": \"Templates\", \"body\": \"Build dynamic HTML with SPL expressions.\", \"tag\": \"Guide\", \"tagColor\": \"#3b82f6\"})\n@render(\"Card\", {\"title\": \"Filters\", \"body\": \"Transform output with 25+ built-in filters.\", \"tag\": \"Popular\", \"tagColor\": \"#ef4444\"})",
			"data":     `{}`,
		},
		{
			"name":     "component-slots",
			"label":    "Components: Slots",
			"category": "Components",
			"tags":     []string{"components", "slots"},
			"template": "@// Component with named slots\n@component(\"Panel\") {\n  <style>.panel { border: 1px solid #ccc; border-radius: 8px; overflow: hidden; margin: 12px 0; } .panel-header { background: #f0f0f0; padding: 8px 16px; font-weight: bold; border-bottom: 1px solid #ccc; } .panel-body { padding: 16px; } .panel-footer { background: #fafafa; padding: 8px 16px; font-size: 12px; color: #666; border-top: 1px solid #ccc; } .status-green { color: green; }</style>\n  <div class=\"panel\">\n    <div class=\"panel-header\">\n      @slot(\"header\")\n    </div>\n    <div class=\"panel-body\">\n      @slot\n    </div>\n    <div class=\"panel-footer\">\n      @slot(\"footer\")\n    </div>\n  </div>\n}\n\n<h1>Named Slots Demo</h1>\n\n@render(\"Panel\") {\n  @fill(\"header\") { User Profile }\n  <p>Name: ${userName}</p>\n  <p>Role: ${role | title}</p>\n  @fill(\"footer\") { Last login: ${lastLogin} }\n}\n\n@render(\"Panel\") {\n  @fill(\"header\") { System Status }\n  <p class=\"status-green\">All systems operational.</p>\n  @fill(\"footer\") { Updated just now }\n}",
			"data":     `{"userName": "Alice", "role": "administrator", "lastLogin": "2 hours ago"}`,
		},
		{
			"name":     "let-computed",
			"label":    "Let & Computed",
			"category": "Advanced Templates",
			"tags":     []string{"computed", "let"},
			"template": "<style>\ntable { border-collapse: collapse; width: 100%; }\nth, td { padding: 8px; }\nth { background: #f0f0f0; text-align: left; }\ntd:nth-child(2) { text-align: center; }\ntd:nth-child(3) { text-align: right; }\ntd:nth-child(4) { text-align: right; font-weight: bold; }\n</style>\n@let(greeting = \"Hello, \" + name + \"!\")\n<h1>${greeting}</h1>\n\n<h2>Order Summary</h2>\n<table>\n  <tr><th>Item</th><th>Qty</th><th>Price</th><th>Total</th></tr>\n@for(item in items) {\n  @computed(lineTotal = item.price * item.qty)\n  <tr><td>${item.name}</td><td>${item.qty}</td><td>$${item.price}</td><td>$${lineTotal}</td></tr>\n}\n</table>",
			"data":     `{"name": "Alice", "items": [{"name": "Widget", "price": 10, "qty": 3}, {"name": "Gadget", "price": 25, "qty": 2}, {"name": "Doohickey", "price": 5, "qty": 10}]}`,
		},
		{
			"name":     "watch",
			"label":    "Watch: Grouped List",
			"category": "Advanced Templates",
			"tags":     []string{"watch", "grouping"},
			"template": "<style>\n.category-header { margin-top: 16px; padding-bottom: 4px; border-bottom: 2px solid #3b82f6; color: #3b82f6; }\n.item { padding: 4px 12px; }\n.sale { color: #ef4444; font-size: 12px; }\n</style>\n@// @watch renders its body only when the expression value changes\n\n<h1>Product Catalog</h1>\n\n@for(item in items) {\n  @watch(item.category) {\n    <h2 class=\"category-header\">${item.category | title}</h2>\n  }\n  <div class=\"item\">\n    ${item.name} — <strong>$${item.price}</strong>\n    @if(item.onSale) { <span class=\"sale\"> SALE</span> }\n  </div>\n}",
			"data":     `{"items": [{"name": "Laptop", "category": "electronics", "price": 999, "onSale": false}, {"name": "Phone", "category": "electronics", "price": 699, "onSale": true}, {"name": "Tablet", "category": "electronics", "price": 499, "onSale": false}, {"name": "Desk", "category": "furniture", "price": 299, "onSale": true}, {"name": "Chair", "category": "furniture", "price": 199, "onSale": false}, {"name": "Novel", "category": "books", "price": 15, "onSale": true}, {"name": "Textbook", "category": "books", "price": 89, "onSale": false}]}`,
		},
		{
			"name":     "reactive-html",
			"label":    "Reactive HTML",
			"category": "Reactive Templates",
			"tags":     []string{"signals", "hydration", "reactive"},
			"template": "<style>\n.container { font-family: sans-serif; max-width: 32rem; margin: 1rem auto; padding: 1rem; border: 1px solid #ddd; border-radius: 12px; }\n.button-row { display: flex; gap: 0.5rem; margin-bottom: 1rem; }\n.dark-panel { color: #ccc; background: #333; padding: 1rem; }\n.effect { }\n.reactive-section { padding: 0.75rem; background: #f6f8fa; border-radius: 8px; }\n.panel-msg { margin-top: 0.5rem; }\n</style>\n@signal(counter = start)\n@signal(panelOpen = false)\n<div class=\"container\">\n  <h1>${title}</h1>\n  <p>Counter: @bind(counter)</p>\n  <div class=\"button-row\">\n    <button on:click=\"counter += 1\">Increment</button>\n    <button on:click=\"toggle(panelOpen)\">Toggle Panel</button>\n  </div>\n  @effect(counter) {\n    <p>Effect sees counter = ${counter}</p>\n  }\n  @reactive(counter, panelOpen) {\n    <section class=\"reactive-section\">\n      <strong>Reactive view count:</strong> ${counter}\n      @if(panelOpen) {\n        <div class=\"panel-msg\">Panel is open</div>\n      } @else {\n        <div class=\"panel-msg\">Panel is closed</div>\n      }\n    </section>\n  }\n</div>",
			"data":     `{"title": "Playground Reactivity", "start": 2}`,
		},
		{
			"name":     "dom-events-bindings",
			"label":    "DOM Events + Bindings",
			"category": "Reactive Templates",
			"tags":     []string{"events", "bindings"},
			"template": "<style>\n.container { font-family: sans-serif; max-width: 36rem; margin: 1rem auto; padding: 1rem; border: 1px solid #ddd; border-radius: 16px; display: grid; gap: 0.75rem; }\n.input { padding: 0.65rem 0.85rem; border: 1px solid #ccc; border-radius: 12px; }\n.label { display: flex; gap: 0.5rem; align-items: center; }\n</style>\n@signal(counter = start)\n@signal(name = initialName)\n@signal(active = false)\n@signal(lastKey = \"none\")\n@reactive(counter, name, active, lastKey) {\n  <div class=\"container\">\n    <h1>${title}</h1>\n    <button on:click.prevent=\"counter += 1\">Increment</button>\n    <input bind:value=\"name\" on:keydown=\"lastKey = event.key\" placeholder=\"Type your name\" class=\"input\" />\n    <label class=\"label\">\n      <input type=\"checkbox\" bind:checked=\"active\" />\n      Active\n    </label>\n    <p bind:textContent=\"name\"></p>\n    <p>Counter: ${counter}</p>\n    <p>Active: ${active}</p>\n    <p>Last key: ${lastKey}</p>\n  </div>\n}",
			"data":     `{"title": "DOM Events + Bindings", "start": 1, "initialName": "SPL"}`,
		},
		{
			"name":     "callbacks-handlers",
			"label":    "Functions, Handlers, Callbacks",
			"category": "Reactive Templates",
			"tags":     []string{"callbacks", "functions", "handlers"},
			"template": "<style>\n.container { font-family: sans-serif; max-width: 38rem; margin: 1rem auto; padding: 1rem; border: 1px solid #ddd; border-radius: 16px; display: grid; gap: 0.75rem; }\n.description { color: #666; }\n.button-row { display: flex; gap: 0.5rem; flex-wrap: wrap; }\n</style>\n@signal(counter = start)\n@signal(lastAction = \"none\")\n@handler(incrementByTwo) {\n  counter += 2;\n  lastAction = 'handler:incrementByTwo';\n}\n@handler(markCallback) {\n  setSignal(lastAction, 'callback-style update');\n  counter += 4;\n}\n@reactive(counter, lastAction) {\n  <div class=\"container\">\n    <h1>${title}</h1>\n    <p class=\"description\">Inline logic, multiline handlers, callback-style helpers, and anonymous functions.</p>\n    <div class=\"button-row\">\n      <button on:click=\"counter += 1; lastAction = 'inline function logic'\">Inline Function Logic</button>\n      <button on:click=\"incrementByTwo\">Multiline Handler</button>\n      <button on:click=\"(() => { counter += 3; lastAction = 'anonymous function'; })\">Anonymous Function</button>\n      <button on:click=\"markCallback\">Callback-style Update</button>\n    </div>\n    <p>Counter: ${counter}</p>\n    <p>Last action: ${lastAction}</p>\n  </div>\n}",
			"data":     `{"title": "Functions, Handlers, Callbacks", "start": 1}`,
		},
		{
			"name":     "template-todo-crud",
			"label":    "Template TODO CRUD",
			"category": "Reactive Templates",
			"tags":     []string{"todo", "crud", "types", "signals"},
			"template": `<style>
.main-container { font-family: system-ui, -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif; max-width: 72rem; margin: 1rem auto; padding: 1.25rem; display: grid; gap: 1rem; color: #0f172a; }
.hero-section { padding: 1.25rem; border-radius: 24px; background: linear-gradient(135deg, #ecfeff 0%, #eff6ff 45%, #fdf4ff 100%); border: 1px solid #bfdbfe; box-shadow: 0 18px 45px rgba(15,23,42,0.1); }
.hero-p1 { margin: 0 0 0.35rem; font-size: 0.78rem; letter-spacing: 0.14em; text-transform: uppercase; color: #0f766e; }
.hero-h1 { margin: 0; font-size: 2rem; line-height: 1.1; }
.hero-p2 { margin: 0.65rem 0 0; max-width: 58rem; color: #334155; }
.types-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(16rem, 1fr)); gap: 0.85rem; margin-top: 1rem; }
.type-article { padding: 0.9rem 1rem; border-radius: 18px; background: rgba(255,255,255,0.82); border: 1px solid #dbeafe; }
.section-label { font-size: 0.74rem; letter-spacing: 0.12em; text-transform: uppercase; color: #64748b; }
.tags-container { display: flex; flex-wrap: wrap; gap: 0.45rem; margin-top: 0.65rem; }
.type-badge { display: inline-flex; align-items: center; padding: 0.3rem 0.6rem; border-radius: 999px; color: white; font-size: 0.78rem; }
.status-badge { display: inline-flex; align-items: center; padding: 0.3rem 0.6rem; border-radius: 999px; background: #e2e8f0; color: #0f172a; font-size: 0.78rem; }
.priority-badge { display: inline-flex; align-items: center; padding: 0.3rem 0.6rem; border-radius: 999px; color: white; font-size: 0.78rem; }
.crud-section { display: grid; grid-template-columns: repeat(auto-fit, minmax(22rem, 1fr)); gap: 1rem; align-items: start; }
.write-article { padding: 1rem; border-radius: 22px; background: white; border: 1px solid #dbeafe; box-shadow: 0 12px 30px rgba(15,23,42,0.06); display: grid; gap: 0.8rem; }
.form-h2 { margin: 0.25rem 0 0; font-size: 1.15rem; }
.title-input { padding: 0.75rem 0.9rem; border-radius: 14px; border: 1px solid #cbd5e1; font-size: 0.94rem; }
.fields-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 0.65rem; }
.field-select { padding: 0.75rem 0.9rem; border-radius: 14px; border: 1px solid #cbd5e1; background: white; }
.notes-textarea { padding: 0.75rem 0.9rem; border-radius: 14px; border: 1px solid #cbd5e1; font-size: 0.94rem; resize: vertical; }
.buttons-container { display: flex; flex-wrap: wrap; gap: 0.6rem; }
.save-button { padding: 0.78rem 1rem; border: none; border-radius: 14px; background: linear-gradient(135deg, #0891b2, #2563eb); color: white; font-weight: 700; cursor: pointer; }
.reset-button { padding: 0.78rem 1rem; border: 1px solid #cbd5e1; border-radius: 14px; background: #f8fafc; color: #0f172a; font-weight: 600; cursor: pointer; }
.draft-box { padding: 0.8rem 0.9rem; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; }
.draft-pre { margin: 0.6rem 0 0; font-size: 0.8rem; white-space: pre-wrap; color: #334155; }
.read-article { padding: 1rem; border-radius: 22px; background: white; border: 1px solid #e2e8f0; box-shadow: 0 12px 30px rgba(15,23,42,0.06); display: grid; gap: 0.8rem; }
.read-header { display: flex; justify-content: space-between; gap: 1rem; align-items: flex-start; flex-wrap: wrap; }
.count-badge { display: inline-flex; align-items: center; padding: 0.35rem 0.7rem; border-radius: 999px; background: #eff6ff; color: #1d4ed8; font-size: 0.78rem; font-weight: 700; }
.activity-box { padding: 0.8rem 0.9rem; border-radius: 16px; background: #0f172a; color: #e2e8f0; border: 1px solid #1e293b; font-size: 0.85rem; }
.activity-strong { color: white; }
.data-grid { display: grid; gap: 0.75rem; }
.json-box { padding: 0.8rem 0.9rem; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; }
.json-pre { margin: 0.6rem 0 0; font-size: 0.8rem; white-space: pre-wrap; color: #334155; min-height: 14rem; }
.actions-box { padding: 0.8rem 0.9rem; border-radius: 16px; background: #f8fafc; border: 1px solid #e2e8f0; display: grid; gap: 0.65rem; }
.actions-input-row { display: flex; flex-wrap: wrap; gap: 0.55rem; align-items: center; }
.id-input { width: 7rem; padding: 0.7rem 0.8rem; border-radius: 12px; border: 1px solid #cbd5e1; font-size: 0.9rem; }
.load-button { padding: 0.6rem 0.85rem; border: none; border-radius: 12px; background: #0f766e; color: white; font-weight: 700; cursor: pointer; }
.delete-button { padding: 0.6rem 0.85rem; border: 1px solid #fecaca; border-radius: 12px; background: #fff1f2; color: #be123c; font-weight: 700; cursor: pointer; }
.help-p { margin: 0; font-size: 0.82rem; color: #475569; }
.empty-p { margin: 0; font-size: 0.82rem; color: #9a3412; }
</style>
@let(todoTypes = [{"value": "feature", "label": "Feature", "color": "#2563eb"}, {"value": "bug", "label": "Bug", "color": "#dc2626"}, {"value": "task", "label": "Task", "color": "#0891b2"}, {"value": "chore", "label": "Chore", "color": "#7c3aed"}])
@let(todoStatuses = [{"value": "todo", "label": "Todo"}, {"value": "in_progress", "label": "In Progress"}, {"value": "done", "label": "Done"}])
@let(todoPriorities = [{"value": "low", "label": "Low", "color": "#64748b"}, {"value": "medium", "label": "Medium", "color": "#d97706"}, {"value": "high", "label": "High", "color": "#be123c"}])

@signal(todos = [
  {"id": 1, "title": "Sketch template-only CRUD flow", "type": "feature", "status": "done", "priority": "high", "assignee": "Ava", "notes": "Types, seed data, and handlers all live in this SPL template."},
  {"id": 2, "title": "Document signal shape", "type": "task", "status": "in_progress", "priority": "medium", "assignee": "Noah", "notes": "Use structured objects for title, type, status, priority, assignee, and notes."},
  {"id": 3, "title": "Polish empty-state messaging", "type": "chore", "status": "todo", "priority": "low", "assignee": "Mia", "notes": "Delete every item to see the read view fall back cleanly."}
])
@signal(todoForm = {"title": "", "type": "feature", "status": "todo", "priority": "medium", "assignee": "Mia", "notes": ""})
@signal(editingID = 0)
@signal(nextID = 4)
@signal(todoActionID = "1")
@signal(todoCount = 3)
@signal(activity = "Template-local types, TODO data, and CRUD handlers are ready.")

@handler(resetForm) {
  setSignal(todoForm, {title: '', type: 'feature', status: 'todo', priority: 'medium', assignee: 'Mia', notes: ''});
  setSignal(editingID, 0);
  setSignal(activity, 'Ready to create a new TODO');
}

@handler(saveTodo) {
  var draft = signal('todoForm') || {};
  var title = (draft.title || '').trim();
  if (!title) {
    setSignal(activity, 'Title is required before saving');
    return;
  }

  if (Number(signal('editingID')) > 0) {
    var id = Number(signal('editingID'));
    var updatedTodos = setSignal(todos, function(prev) {
      return (prev || []).map(function(item) {
        if (item.id !== id) {
          return item;
        }
        return {
          id: item.id,
          title: title,
          type: draft.type || 'task',
          status: draft.status || 'todo',
          priority: draft.priority || 'medium',
          assignee: (draft.assignee || 'Unassigned').trim() || 'Unassigned',
          notes: draft.notes || ''
        };
      });
    });
    setSignal(todoCount, (updatedTodos || []).length);
    resetForm();
    setSignal(activity, 'Updated TODO #' + id);
    return;
  }

  var createdID = Number(signal('nextID'));
  var createdTodos = setSignal(todos, function(prev) {
    return (prev || []).concat([{
      id: createdID,
      title: title,
      type: draft.type || 'task',
      status: draft.status || 'todo',
      priority: draft.priority || 'medium',
      assignee: (draft.assignee || 'Unassigned').trim() || 'Unassigned',
      notes: draft.notes || ''
    }]);
  });
  setSignal(todoCount, (createdTodos || []).length);
  setSignal(nextID, createdID + 1);
  resetForm();
  setSignal(activity, 'Created TODO #' + createdID);
}

@handler(editTodo) {
  var target = event && event.currentTarget;
  var rawID = target ? target.getAttribute('data-id') : '';
  if (!rawID) {
    rawID = signal('todoActionID');
  }
  var id = Number(rawID);
  if (!id) {
    setSignal(activity, 'Enter a valid TODO id before editing');
    return;
  }
  var item = (signal('todos') || []).find(function(entry) {
    return entry.id === id;
  });
  if (!item) {
    setSignal(activity, 'TODO #' + id + ' was not found');
    return;
  }
  setSignal(editingID, id);
  setSignal(todoForm, {
    title: item.title || '',
    type: item.type || 'task',
    status: item.status || 'todo',
    priority: item.priority || 'medium',
    assignee: item.assignee || 'Unassigned',
    notes: item.notes || ''
  });
  setSignal(activity, 'Editing TODO #' + id);
}

@handler(deleteTodo) {
  var target = event && event.currentTarget;
  var rawID = target ? target.getAttribute('data-id') : '';
  if (!rawID) {
    rawID = signal('todoActionID');
  }
  var id = Number(rawID);
  if (!id) {
    setSignal(activity, 'Enter a valid TODO id before deleting');
    return;
  }
  var remainingTodos = setSignal(todos, function(prev) {
    return (prev || []).filter(function(item) {
      return item.id !== id;
    });
  });
  setSignal(todoCount, (remainingTodos || []).length);
  if (Number(signal('editingID')) === id) {
    resetForm();
  }
  setSignal(activity, 'Deleted TODO #' + id);
}

<div class="main-container">
  <section class="hero-section">
    <p class="hero-p1">Self-contained template</p>
    <h1 class="hero-h1">Typed TODO CRUD inside SPL</h1>
    <p class="hero-p2">This example keeps enum-like type definitions, seed TODO data, and create-read-update-delete handlers inside the template itself. The JSON data panel stays empty.</p>
    <div class="types-grid">
      <article class="type-article">
        <div class="section-label">TODO Types</div>
        <div class="tags-container">
          @for(type in todoTypes) {
            <span class="type-badge" style="background:${type.color}">${type.label}</span>
          }
        </div>
      </article>
      <article class="type-article">
        <div class="section-label">Statuses</div>
        <div class="tags-container">
          @for(status in todoStatuses) {
            <span class="status-badge">${status.label}</span>
          }
        </div>
      </article>
      <article class="type-article">
        <div class="section-label">Priorities</div>
        <div class="tags-container">
          @for(priority in todoPriorities) {
            <span class="priority-badge" style="background:${priority.color}">${priority.label}</span>
          }
        </div>
      </article>
    </div>
  </section>

  @reactive(todoForm, editingID, activity, todoCount, todoActionID) {
    <section class="crud-section">
      <article class="write-article">
        <div>
          <div class="section-label">Write</div>
          @if(editingID) {
            <h2 class="form-h2">Update TODO #${editingID}</h2>
          } @else {
            <h2 class="form-h2">Create a TODO</h2>
          }
        </div>

        <input type="text" data-spl-model="todoForm.title" placeholder="Title" class="title-input" />

        <div class="fields-grid">
          <select data-spl-model="todoForm.type" class="field-select">
            @for(type in todoTypes) {
              <option value="${type.value}">${type.label}</option>
            }
          </select>
          <select data-spl-model="todoForm.priority" class="field-select">
            @for(priority in todoPriorities) {
              <option value="${priority.value}">${priority.label}</option>
            }
          </select>
          <select data-spl-model="todoForm.status" class="field-select">
            @for(status in todoStatuses) {
              <option value="${status.value}">${status.label}</option>
            }
          </select>
          <input type="text" data-spl-model="todoForm.assignee" placeholder="Assignee" class="title-input" />
        </div>

        <textarea rows="4" data-spl-model="todoForm.notes" placeholder="Notes" class="notes-textarea"></textarea>

        <div class="buttons-container">
          <button type="button" on:click="saveTodo" class="save-button">
            @if(editingID) { Save Changes } @else { Create TODO }
          </button>
          <button type="button" on:click="resetForm" class="reset-button">Reset Form</button>
        </div>

        <div class="draft-box">
          <div class="section-label">Current Draft</div>
          <pre class="draft-pre">${todoForm}</pre>
        </div>
      </article>

      <article class="read-article">
        <div class="read-header">
          <div>
            <div class="section-label">Read</div>
            <h2 class="form-h2">Live TODO State</h2>
          </div>
          <span class="count-badge">@bind(todoCount) items</span>
        </div>

        <div class="activity-box">
          <strong class="activity-strong">Activity:</strong> ${activity}
        </div>

        <div class="data-grid">
          <div class="json-box">
            <div class="section-label">Live JSON</div>
            <pre data-spl-bind="todos" data-spl-attr="textContent" class="json-pre">[]</pre>
          </div>

          <div class="actions-box">
            <div class="section-label">Update / Delete by Id</div>
            <div class="actions-input-row">
              <input type="text" data-spl-model="todoActionID" placeholder="TODO id" class="id-input" />
              <button type="button" on:click="editTodo" class="load-button">Load Into Form</button>
              <button type="button" on:click="deleteTodo" class="delete-button">Delete By Id</button>
            </div>
            @if(todoCount) {
              <p class="help-p">Use any id visible in the live JSON payload to update or delete an item.</p>
            } @else {
              <p class="empty-p">No TODO items remain. Create a new one to restore the collection.</p>
            }
          </div>
        </div>
      </article>
    </section>
  }
</div>`,
			"data": `{}`,
		},
	}
}

// ────────────────────────── config ──────────────────────────

type config struct {
	Addr            string
	MaxBodyBytes    int64
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
	RateLimit       int
	RateWindow      time.Duration
	TrustProxy      bool
}

func loadConfig() config {
	return config{
		Addr:            envOr("PLAYGROUND_ADDR", ":8090"),
		MaxBodyBytes:    envInt64("PLAYGROUND_MAX_BODY_BYTES", 1<<20),
		ReadTimeout:     10 * time.Second,
		WriteTimeout:    30 * time.Second,
		IdleTimeout:     120 * time.Second,
		ShutdownTimeout: 10 * time.Second,
		RateLimit:       envInt("PLAYGROUND_RATE_LIMIT", 60),
		RateWindow:      time.Minute,
		TrustProxy:      envBool("PLAYGROUND_TRUST_PROXY"),
	}
}

// ────────────────────────── rate limiter ──────────────────────────

type rateLimiter struct {
	mu     sync.Mutex
	counts map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{counts: make(map[string][]time.Time), limit: limit, window: window}
}

func (rl *rateLimiter) allow(client string, now time.Time) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := now.Add(-rl.window)
	hits := rl.counts[client]
	filtered := hits[:0]
	for _, t := range hits {
		if t.After(cutoff) {
			filtered = append(filtered, t)
		}
	}
	if len(filtered) >= rl.limit {
		rl.counts[client] = filtered
		return false
	}
	rl.counts[client] = append(filtered, now)
	return true
}

func (rl *rateLimiter) prune(now time.Time) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	cutoff := now.Add(-rl.window)
	for k, hits := range rl.counts {
		filtered := hits[:0]
		for _, t := range hits {
			if t.After(cutoff) {
				filtered = append(filtered, t)
			}
		}
		if len(filtered) == 0 {
			delete(rl.counts, k)
		} else {
			rl.counts[k] = filtered
		}
	}
}

// ────────────────────────── helpers ──────────────────────────

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
		return n
	}
	return fallback
}

func envInt64(key string, fallback int64) int64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	var n int64
	if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
		return n
	}
	return fallback
}

func envBool(key string) bool {
	v := strings.ToLower(os.Getenv(key))
	return v == "true" || v == "1" || v == "yes"
}

// ────────────────────────── main ──────────────────────────

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	cfg := loadConfig()
	rl := newRateLimiter(cfg.RateLimit, cfg.RateWindow)
	go startRateLimiterCleanup(rl, cfg.RateWindow)

	mux := http.NewServeMux()

	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})

	mux.HandleFunc("/api/examples", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"template_examples": builtinExamples(),
		})
	})

	mux.HandleFunc("/api/render", func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		if r.Method != http.MethodPost {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
			return
		}

		clientID := clientKey(r, cfg.TrustProxy)
		if !rl.allow(clientID, time.Now()) {
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate limit exceeded"})
			return
		}

		if ct := strings.TrimSpace(r.Header.Get("Content-Type")); ct != "" && !strings.HasPrefix(strings.ToLower(ct), "application/json") {
			writeJSON(w, http.StatusUnsupportedMediaType, map[string]any{"error": "content type must be application/json"})
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, cfg.MaxBodyBytes)
		var req renderRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			if errors.Is(err, io.EOF) {
				writeJSON(w, http.StatusBadRequest, map[string]any{"error": "request body is empty"})
				return
			}
			var maxErr *http.MaxBytesError
			if errors.As(err, &maxErr) {
				writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"error": "payload too large"})
				return
			}
			writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid json payload"})
			return
		}
		if strings.TrimSpace(req.Template) == "" {
			writeJSON(w, http.StatusBadRequest, renderResponse{Error: "template is required", ErrorKind: "validation"})
			return
		}

		var data map[string]any
		if strings.TrimSpace(req.Data) != "" {
			if err := json.Unmarshal([]byte(req.Data), &data); err != nil {
				writeJSON(w, http.StatusBadRequest, renderResponse{Error: "invalid data JSON: " + err.Error(), ErrorKind: "validation"})
				return
			}
		}

		cwd, err := os.Getwd()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "failed to resolve working directory"})
			return
		}

		engine := template.New()
		engine.BaseDir = cwd
		engine.AutoEscape = true

		renderStart := time.Now()
		rendered, renderErr := engine.RenderSSR(req.Template, data)
		duration := time.Since(renderStart).Milliseconds()

		_ = start
		if renderErr != nil {
			writeJSON(w, http.StatusBadRequest, renderResponse{Error: renderErr.Error(), ErrorKind: "template", DurationMS: duration})
			return
		}

		writeJSON(w, http.StatusOK, renderResponse{Result: rendered, ResultType: "HTML", DurationMS: duration})
	})

	fileServer, err := fsSub()
	if err != nil {
		logger.Error("failed to load embedded static files", slog.String("error", err.Error()))
		os.Exit(2)
	}
	mux.Handle("/", fileServer)

	handler := withRecovery(logger, withSecurityHeaders(mux))
	server := &http.Server{
		Addr:         cfg.Addr,
		Handler:      handler,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			logger.Error("graceful shutdown failed", slog.String("error", err.Error()))
		}
	}()

	logger.Info("SPL Template Playground running", slog.String("addr", cfg.Addr))
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Error("server terminated", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func clientKey(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if ip := strings.TrimSpace(strings.Split(strings.TrimSpace(r.Header.Get("X-Forwarded-For")), ",")[0]); ip != "" {
			if parsed := net.ParseIP(ip); parsed != nil {
				return parsed.String()
			}
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return "unknown"
}

func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func withRecovery(logger *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error("panic recovered", slog.Any("panic", rec), slog.String("path", r.URL.Path))
				writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "internal server error"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func startRateLimiterCleanup(rl *rateLimiter, every time.Duration) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	for now := range ticker.C {
		rl.prune(now)
	}
}

func fsSub() (http.Handler, error) {
	fsys, err := staticFS.ReadFile("static/index.html")
	if err != nil || len(fsys) == 0 {
		return nil, err
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		path = filepath.Clean(path)
		if strings.Contains(path, "..") {
			http.NotFound(w, r)
			return
		}
		if path == "index.html" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fsys)
			return
		}
		content, err := staticFS.ReadFile("static/" + path)
		if err != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(fsys)
			return
		}
		switch {
		case strings.HasSuffix(path, ".js"):
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		case strings.HasSuffix(path, ".css"):
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		case strings.HasSuffix(path, ".html"):
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
		}
		_, _ = w.Write(content)
	}), nil
}

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(payload)
}
