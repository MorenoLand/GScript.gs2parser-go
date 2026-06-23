package gs2parser

import (
	"errors"
	"strings"
	"testing"
)

func TestCompileDetailedParserDiagnostic(t *testing.T) {
	src := "function onCreated() {\n  temp.x = 1\n  temp.y = 2;\n}"
	res := CompileDetailed(src)
	if res == nil {
		t.Fatal("expected result")
	}
	if len(res.Bytecode) != 0 {
		t.Fatal("expected no bytecode")
	}
	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if d.Severity != "error" || d.Stage != "parser" {
		t.Fatalf("unexpected diagnostic kind: %#v", d)
	}
	if d.Line != 3 || d.Column != 2 || d.Near != "temp" {
		t.Fatalf("unexpected location: %#v", d)
	}
	if d.SourceLine != "  temp.y = 2;" {
		t.Fatalf("unexpected source line %q", d.SourceLine)
	}
	if !strings.Contains(d.Message, "expected ;") {
		t.Fatalf("unexpected message %q", d.Message)
	}
}

func TestCompileDetailedLexerDiagnostic(t *testing.T) {
	src := "function onCreated() {\n  temp.x = 1;\n  `\n}"
	res := CompileDetailed(src)
	if len(res.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(res.Diagnostics))
	}
	d := res.Diagnostics[0]
	if d.Stage != "lexer" || d.Line != 3 || d.Column != 2 || d.Near != "`" {
		t.Fatalf("unexpected lexer diagnostic: %#v", d)
	}
}

func TestCompileReturnsDiagnosticError(t *testing.T) {
	_, err := Compile("function onCreated() {\n  temp.x = 1\n}")
	if err == nil {
		t.Fatal("expected error")
	}
	var diagnosticErr *DiagnosticError
	if !errors.As(err, &diagnosticErr) {
		t.Fatalf("expected DiagnosticError, got %T", err)
	}
	if len(diagnosticErr.Diagnostics) != 1 {
		t.Fatalf("expected 1 diagnostic, got %d", len(diagnosticErr.Diagnostics))
	}
}

func TestCompileDetailedSuccess(t *testing.T) {
	res := CompileDetailed("function onCreated() {\n  temp.x = 1;\n}")
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", res.Diagnostics)
	}
	if len(res.Bytecode) == 0 || res.AST == nil {
		t.Fatal("expected bytecode and ast")
	}
}

func TestCompileDetailedTabIdentifier(t *testing.T) {
	src := "function onCreated() {\n  tab = 0;\n  profile.tab = false;\n  for (temp.tab: temp.tabs) { temp.x = 1; }\n}"
	res := CompileDetailed(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", res.Diagnostics)
	}
}

func TestCompileDetailedSupportsCommonGS2CompatibilitySyntax(t *testing.T) {
	src := `function onCreated() {
  temp.low = player.account.lower();
  temp.up = player.account.upper();
  temp.tail = player.account.substring(3);
  foo(), temp.ready = false;
  temp.es = 4;
  this.logtypes = {"a", "b"};
  temp.a = new [this.logtypes.size()];
  temp.b = new [temp.es];
  temp.c = new [3 * 10];
  temp.grid = new[temp.es][3 * 10];
  new GuiWindowCtrl(Test_Window) {
    if (temp.ready == false) temp.ready = true;
    for (temp.i = 0; temp.i < 2; temp.i++) temp.ready = temp.i;
  }
  do {
    temp.es--;
  } while (temp.es > 0);
  hideimgs(200, 210);
  temp.k = keycode(z);
}`
	res := CompileDetailed(src)
	if len(res.Diagnostics) != 0 {
		t.Fatalf("unexpected diagnostics: %#v", res.Diagnostics)
	}
	if len(res.Bytecode) == 0 {
		t.Fatal("expected bytecode")
	}
}
