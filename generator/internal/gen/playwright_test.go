package gen

import (
	"strings"
	"testing"
)

func TestEmitPlaywright(t *testing.T) {
	ir, diags, err := Build("../../../examples/search")
	if err != nil || HasErrors(diags) {
		t.Fatalf("build: %v %v", err, diags)
	}
	js, types, err := EmitPlaywright(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "export function createSightkick") || !strings.Contains(types, `"search": (params: { "query": string; })`) {
		t.Fatalf("unexpected output declarations: %s", types)
	}
	js2, types2, _ := EmitPlaywright(ir)
	if js != js2 || types != types2 {
		t.Fatal("non-deterministic emission")
	}
	ir.Tools[0].Mode = "api"
	if _, _, err := EmitPlaywright(ir); err == nil {
		t.Fatal("accepted unsupported API mode")
	}
}

func TestEmitPlaywrightEscapesNames(t *testing.T) {
	ir := IR{Version: 1, Tools: []Tool{{Name: "__proto__", Mode: "live", InputSchema: InputSchema{Type: "object", Properties: map[string]SchemaProp{"__proto__": {Type: "string"}, "quote\"": {Type: "string", Enum: []string{"\";throw 1;//"}}}}}}, Views: []ViewRef{{Name: "constructor", Route: "/"}}}
	js, types, err := EmitPlaywright(ir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js, "const ir = JSON.parse(") || !strings.Contains(types, `"__proto__"?`) || !strings.Contains(types, `"constructor":`) {
		t.Fatalf("unsafe names: %s", types)
	}
}
