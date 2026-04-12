package template

import (
	"fmt"
	htmltemplate "html/template"
	"strings"
	"testing"
)

// --- SPL benchmarks ---

func BenchmarkSPL_SimpleExpr(b *testing.B) {
	e := New()
	e.AutoEscape = false
	data := map[string]any{"title": "Hello World", "subtitle": "Welcome"}
	tmpl := `<h1>${title}</h1><p>${subtitle}</p>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Render(tmpl, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSPL_Loop100(b *testing.B) {
	e := New()
	e.AutoEscape = false
	items := make([]any, 100)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprintf("Item %d", i), "price": i * 10}
	}
	data := map[string]any{"items": items}
	tmpl := `<ul>@for(item in items) {<li>${item.name}: $${item.price}</li>}</ul>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Render(tmpl, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSPL_Loop1000(b *testing.B) {
	e := New()
	e.AutoEscape = false
	items := make([]any, 1000)
	for i := range items {
		items[i] = map[string]any{"name": fmt.Sprintf("Item %d", i), "price": i * 10}
	}
	data := map[string]any{"items": items}
	tmpl := `<ul>@for(item in items) {<li>${item.name}: $${item.price}</li>}</ul>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Render(tmpl, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSPL_Conditional(b *testing.B) {
	e := New()
	e.AutoEscape = false
	data := map[string]any{"status": "active", "name": "Alice", "role": "admin"}
	tmpl := `@if(status == "active") {<span class="active">${name}</span>} @elseif(status == "pending") {<span class="pending">${name}</span>} @else {<span class="inactive">${name}</span>}@if(role == "admin") { <em>Admin</em>}`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Render(tmpl, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSPL_Component(b *testing.B) {
	e := New()
	e.AutoEscape = false
	e.RegisterComponent("Card", `<div class="card"><h3>${title}</h3><p>${body}</p></div>`)
	data := map[string]any{}
	tmpl := `@render("Card", {"title": "Hello", "body": "World"})@render("Card", {"title": "Foo", "body": "Bar"})`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Render(tmpl, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkSPL_ComplexPage(b *testing.B) {
	e := New()
	e.AutoEscape = false
	e.RegisterComponent("ProductCard", `<div class="product"><strong>${name}</strong><span>$${price}</span>@if(onSale) {<em>SALE</em>}</div>`)

	items := make([]any, 50)
	for i := range items {
		items[i] = map[string]any{
			"name":   fmt.Sprintf("Product %d", i),
			"price":  float64(i)*9.99 + 1,
			"onSale": i%3 == 0,
		}
	}
	data := map[string]any{
		"title":    "My Shop",
		"items":    items,
		"loggedIn": true,
		"user":     "Alice",
	}
	tmpl := `<html><head><title>${title}</title></head><body><h1>${title}</h1>@if(loggedIn) {<p>Welcome, ${user}!</p>}<div class="products">@for(item in items) {@render("ProductCard", item)}</div></body></html>`
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := e.Render(tmpl, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

// --- html/template benchmarks for comparison ---

var goSimpleTmpl = htmltemplate.Must(htmltemplate.New("simple").Parse(`<h1>{{.Title}}</h1><p>{{.Subtitle}}</p>`))

func BenchmarkGoHTML_SimpleExpr(b *testing.B) {
	data := struct{ Title, Subtitle string }{"Hello World", "Welcome"}
	var buf strings.Builder
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		err := goSimpleTmpl.Execute(&buf, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

type loopItem struct {
	Name  string
	Price int
}

var goLoop100Tmpl = htmltemplate.Must(htmltemplate.New("loop100").Parse(`<ul>{{range .Items}}<li>{{.Name}}: ${{.Price}}</li>{{end}}</ul>`))

func BenchmarkGoHTML_Loop100(b *testing.B) {
	items := make([]loopItem, 100)
	for i := range items {
		items[i] = loopItem{Name: fmt.Sprintf("Item %d", i), Price: i * 10}
	}
	data := struct{ Items []loopItem }{items}
	var buf strings.Builder
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		err := goLoop100Tmpl.Execute(&buf, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

var goLoop1000Tmpl = htmltemplate.Must(htmltemplate.New("loop1000").Parse(`<ul>{{range .Items}}<li>{{.Name}}: ${{.Price}}</li>{{end}}</ul>`))

func BenchmarkGoHTML_Loop1000(b *testing.B) {
	items := make([]loopItem, 1000)
	for i := range items {
		items[i] = loopItem{Name: fmt.Sprintf("Item %d", i), Price: i * 10}
	}
	data := struct{ Items []loopItem }{items}
	var buf strings.Builder
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		err := goLoop1000Tmpl.Execute(&buf, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

var goCondTmpl = htmltemplate.Must(htmltemplate.New("cond").Parse(`{{if eq .Status "active"}}<span class="active">{{.Name}}</span>{{else if eq .Status "pending"}}<span class="pending">{{.Name}}</span>{{else}}<span class="inactive">{{.Name}}</span>{{end}}{{if eq .Role "admin"}} <em>Admin</em>{{end}}`))

func BenchmarkGoHTML_Conditional(b *testing.B) {
	data := struct{ Status, Name, Role string }{"active", "Alice", "admin"}
	var buf strings.Builder
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		err := goCondTmpl.Execute(&buf, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}

type productItem struct {
	Name   string
	Price  float64
	OnSale bool
}

var goComplexTmpl = htmltemplate.Must(htmltemplate.New("complex").Parse(`<html><head><title>{{.Title}}</title></head><body><h1>{{.Title}}</h1>{{if .LoggedIn}}<p>Welcome, {{.User}}!</p>{{end}}<div class="products">{{range .Items}}<div class="product"><strong>{{.Name}}</strong><span>${{.Price}}</span>{{if .OnSale}}<em>SALE</em>{{end}}</div>{{end}}</div></body></html>`))

func BenchmarkGoHTML_ComplexPage(b *testing.B) {
	items := make([]productItem, 50)
	for i := range items {
		items[i] = productItem{
			Name:   fmt.Sprintf("Product %d", i),
			Price:  float64(i)*9.99 + 1,
			OnSale: i%3 == 0,
		}
	}
	data := struct {
		Title    string
		Items    []productItem
		LoggedIn bool
		User     string
	}{"My Shop", items, true, "Alice"}
	var buf strings.Builder
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		err := goComplexTmpl.Execute(&buf, data)
		if err != nil {
			b.Fatal(err)
		}
	}
}
