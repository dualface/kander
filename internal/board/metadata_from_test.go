package board

import "testing"

func TestMetadataFromReusesCompiledPatternWithoutChangingSemantics(t *testing.T) {
	text := "# Title\n\n- TYPE: Feature\n- SIZE: small\n- LANGUAGE: zh-CN\n- OWNER:\n- 类型: Bug\n"
	if got := MetadataFrom(text, FieldType); got != "Feature" {
		t.Fatalf("first TYPE %q", got)
	}
	if got := MetadataFrom(text, FieldType); got != "Feature" {
		t.Fatalf("cached TYPE %q", got)
	}
	if got := MetadataFrom(text, FieldSize); got != "small" {
		t.Fatalf("SIZE %q", got)
	}
	if got := MetadataFrom(text, FieldLanguage); got != "zh-CN" {
		t.Fatalf("LANGUAGE %q", got)
	}
	if got := MetadataFrom(text, FieldOwner); got != "" {
		t.Fatalf("empty OWNER %q", got)
	}

	legacy := "# 旧\n\n- 类型: Chore\n- SIZE: large\n"
	if got := MetadataFrom(legacy, FieldType); got != "Chore" {
		t.Fatalf("legacy TYPE %q", got)
	}

	wrapped := "- TYPE: one\nstill-type\n- SIZE: small\n"
	if got := MetadataFrom(wrapped, FieldType); got != "one" {
		t.Fatalf("cross-line TYPE %q", got)
	}
}
