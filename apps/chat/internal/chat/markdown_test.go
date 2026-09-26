package chat

import (
	"strings"
	"testing"
)

func TestRenderStripsScripts(t *testing.T) {
	html := Render("**hi** <script>alert(1)</script>")
	if !strings.Contains(html, "<strong>hi</strong>") {
		t.Fatalf("html = %q", html)
	}
	if strings.Contains(html, "script") {
		t.Fatalf("script survived: %q", html)
	}
}

func TestRenderTightHeadings(t *testing.T) {
	spaced := Render("### 2. Hello")
	tight := Render("###2. Hello")
	for _, html := range []string{spaced, tight} {
		if !strings.Contains(html, "<h3>") || !strings.Contains(html, "2. Hello") {
			t.Fatalf("html = %q", html)
		}
	}
	if strings.Contains(tight, "###2") {
		t.Fatalf("tight = %q", tight)
	}
	if strings.Contains(spaced, "<a>") {
		t.Fatalf("dead anchor: %q", spaced)
	}
	mid := Render("Intro\n\n###2. Hello\n")
	if !strings.Contains(mid, "<h3>2. Hello</h3>") {
		t.Fatalf("mid = %q", mid)
	}
}

func TestRenderBold(t *testing.T) {
	full := Render("**Sim, dá pra pedir, mas com jeitinho e respeito.**")
	if !strings.Contains(full, "<strong>") || strings.Contains(full, "**Sim") {
		t.Fatalf("full = %q", full)
	}
	bold := Render("**Quantos lounges principais tem?** 4 lounges de embarque:")
	if !strings.Contains(bold, "<strong>Quantos lounges principais tem?</strong>") ||
		!strings.Contains(bold, "4 lounges") {
		t.Fatalf("bold = %q", bold)
	}
}

func TestRenderLinks(t *testing.T) {
	html := Render("[Example](https://example.com/x) and https://example.com/docs plus [m](mailto:a@b.co)")
	for _, want := range []string{
		`href="https://example.com/x"`, `href="https://example.com/docs"`,
		`href="mailto:a@b.co"`, `target="_blank"`, `rel="noopener noreferrer nofollow"`,
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %q", want, html)
		}
	}
	bad := Render("[click](javascript:alert(1)) [d](data:text/html,hi)")
	for _, banned := range []string{"javascript", "data:text", "target="} {
		if strings.Contains(bad, banned) {
			t.Fatalf("unsafe survived: %q", bad)
		}
	}
}

func TestRenderCodeAndTables(t *testing.T) {
	html := Render("```ruby\nputs 1\n```\n")
	if !strings.Contains(html, `class="language-ruby"`) {
		t.Fatalf("html = %q", html)
	}
	aligned := Render("| a |\n|:---:|\n| x |\n")
	if !strings.Contains(aligned, `align="center"`) {
		t.Fatalf("aligned = %q", aligned)
	}
	code := Render("```sh\n#!/bin/bash\nusers.map(&:name)\n#not a heading\n```\nVisit https://example.com/docs now.")
	for _, want := range []string{"#!/bin/bash", "users.map", "#not a heading", `href="https://example.com/docs"`} {
		if !strings.Contains(code, want) {
			t.Fatalf("missing %q in %q", want, code)
		}
	}
	inline := Render("Use `x = [[1]]` and `myVar` ok.")
	if !strings.Contains(inline, "<code>x = [[1]]</code>") || !strings.Contains(inline, "<code>myVar</code>") {
		t.Fatalf("inline = %q", inline)
	}
	nested := Render("- a\n  - b\n")
	if strings.Count(nested, "<ul>") != 2 {
		t.Fatalf("nested = %q", nested)
	}
	indented := Render("Para:\n\n    code_line(1)\n")
	if !strings.Contains(indented, "<pre><code>code_line(1)") {
		t.Fatalf("indented = %q", indented)
	}
}

func TestRenderLeavesProseAlone(t *testing.T) {
	html := Render("AparênciaJovem de15 anos. PersonalidadeBondoso.")
	for _, want := range []string{"AparênciaJovem", "de15 anos", "PersonalidadeBondoso"} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %q", want, html)
		}
	}
	plain := Render("See https://example.com/docs, e.g. file.txt, GPT4 and H2O.")
	for _, want := range []string{"e.g.", "file.txt", "GPT4", "H2O", `href="https://example.com/docs"`} {
		if !strings.Contains(plain, want) {
			t.Fatalf("missing %q in %q", want, plain)
		}
	}
}

func TestRenderGluedBlocks(t *testing.T) {
	table := Render("### Principais diferenças| Aspecto | Orca |\n|---|---|\n| x | y |\n")
	for _, want := range []string{"<h3>Principais diferenças</h3>", "<table>", "<th>Aspecto</th>"} {
		if !strings.Contains(table, want) {
			t.Fatalf("missing %q in %q", want, table)
		}
	}
	if strings.Contains(table, "diferenças|") {
		t.Fatalf("glue remains: %q", table)
	}
	pipes := Render("### A | B\n\nSome text\n")
	if !strings.Contains(pipes, "<h3>A | B</h3>") || strings.Contains(pipes, "<table>") {
		t.Fatalf("pipes = %q", pipes)
	}
	list := Render("### Resumo- item one\n- item two\n")
	for _, want := range []string{"<h3>Resumo</h3>", "<ul>", "<li>item one</li>", "<li>item two</li>"} {
		if !strings.Contains(list, want) {
			t.Fatalf("missing %q in %q", want, list)
		}
	}
	ordered := Render("### Ranking1. Ana\n2. Bia\n")
	if !strings.Contains(ordered, "<h3>Ranking</h3>") || !strings.Contains(ordered, "<ol>") {
		t.Fatalf("ordered = %q", ordered)
	}
	hyphen := Render("### Pré- processamento\nTexto\n")
	if !strings.Contains(hyphen, "<h3>Pré- processamento</h3>") || strings.Contains(hyphen, "<ul>") {
		t.Fatalf("hyphen = %q", hyphen)
	}
	spaced := Render("### Prós - contras\n- item\n")
	if !strings.Contains(spaced, "<h3>Prós - contras</h3>") || !strings.Contains(spaced, "<li>item</li>") {
		t.Fatalf("spaced = %q", spaced)
	}
}

func TestRenderMidlineLists(t *testing.T) {
	md := "**Quantos lounges principais tem?** 4 lounges de embarque:" +
		"- Lounge 1 (Schengen)- Lounge 2 (não-Schengen, perto de D/E)" +
		"- Lounge 3 (não-Schengen, perto de F/G)- Lounge 4O Mc Donald's está no 2 e no 3."
	html := Render(md)
	for _, want := range []string{
		"<strong>Quantos lounges principais tem?</strong>", "<ul>",
		"<li>Lounge 1 (Schengen)</li>", "<li>Lounge 2 (não-Schengen, perto de D/E)</li>",
		"<li>Lounge 3 (não-Schengen, perto de F/G)</li>",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("missing %q in %q", want, html)
		}
	}
	if n := strings.Count(html, "<li>"); n != 4 {
		t.Fatalf("li count = %d in %q", n, html)
	}
	prose := Render("Ele disse- vai embora. Prós - contras ficam. Área *airside* (após) e *x* (y).")
	if strings.Contains(prose, "<ul>") || !strings.Contains(prose, "<em>airside</em>") {
		t.Fatalf("prose = %q", prose)
	}
	rows := Render("| (a)- x | (b)- y |\n|:---|:---|\n| 1 | 2 |\n")
	if !strings.Contains(rows, "<table>") {
		t.Fatalf("rows = %q", rows)
	}
	fenced := Render("```\nx:- a (c)- b\n```\n")
	if !strings.Contains(fenced, "x:- a (c)- b") {
		t.Fatalf("fenced = %q", fenced)
	}
}
